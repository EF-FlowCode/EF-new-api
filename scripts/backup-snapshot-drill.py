#!/usr/bin/env python3
"""Real restore drill for the zfs_snapshot maintenance backup. Run on the VM as root.

The snapshot is taken through the controller's own path (sync + fsfreeze of only
the database filesystem + host guard), so the drill proves exactly what a
maintenance release does. A clone of it is attached to this VM and restored
into an isolated MySQL and the production application image on an internal
Docker network, then compared with production. Production keeps serving; only
the database filesystem is frozen for about a second, and the comparison reads
add load comparable to an ordinary report query.

Everything created is removed afterwards (the guard destroys only drill
snapshots); the evidence file is kept. Credentials never enter argv or output.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('rolling', HERE / 'rolling-deploy.py')
rolling = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rolling)

DEVICE = Path('/dev/disk/by-id/scsi-0QEMU_QEMU_HARDDISK_drive-scsi2')
MOUNT = Path('/mnt/eflow-mysql-drill')
MYSQL = 'eflow-drill-mysql'
REDIS = 'eflow-drill-redis'
WEB = 'eflow-drill-web'
DATABASE = 'new_api'
# Recent log rows may still be settled after insert; compare the stable history.
TAIL_GAP = 5000
WINDOW = 1_000_000
STATIC_ROW_LIMIT = 2_000_000
SCHEMA_QUERY = ("SELECT TABLE_NAME, COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_DEFAULT, '<null>') "
                "FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = 'new_api' ORDER BY 1, 3; "
                "SELECT TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE "
                "FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = 'new_api' ORDER BY 1, 2, 3")


def sh(*args, timeout=600, env=None, check=True):
    result = subprocess.run(args, capture_output=True, text=True, encoding='utf-8', timeout=timeout,
                            env={**os.environ, **(env or {})})
    if check and result.returncode:
        raise RuntimeError(f'{" ".join(args[:2])} failed (exit {result.returncode})')
    return result.stdout


def file_hash(path):
    with open(path, 'rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


class Drill:
    def __init__(self, root):
        self.deployment = rolling.Deployment(root)
        self.config = self.deployment.snapshot_config()
        production = self.deployment.inspect(self.config['mysql_container'])
        env = dict(item.split('=', 1) for item in production['Config']['Env'] if '=' in item)
        self.secret = {'MYSQL_PWD': env['MYSQL_ROOT_PASSWORD']}
        self.mysql_image = production['Image']
        self.my_cnf = next(m['Source'] for m in production['Mounts'] if m['Destination'] == '/etc/my.cnf')
        self.name = 'drill-' + time.strftime('%Y%m%d%H%M%S', time.gmtime())
        self.network = 'eflow-drill-' + self.name[len('drill-'):]
        self.private = Path(tempfile.mkdtemp(prefix='eflow-drill-'))
        self.attached = False
        self.evidence = {'schema': 1, 'result': 'failed', 'host': socket.gethostname(), 'started': time.time(),
                         'target': {key: self.config[key] for key in ('dataset', 'fs_uuid', 'mount')},
                         'checks': {}, 'cleanup': {}}

    def sql(self, container, query, timeout=1800):
        out = sh('docker', 'exec', '-e', 'MYSQL_PWD', container, 'mysql', '-uroot', '-N', '-B',
                 '--default-character-set=utf8mb4', DATABASE,
                 '-e', 'SET SESSION max_execution_time = 0; SET SESSION information_schema_stats_expiry = 0; ' + query,
                 env=self.secret, timeout=timeout)
        return [line.split('\t') for line in out.splitlines()]

    def wait(self, check, seconds, what):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            try:
                if check():
                    return
            except (RuntimeError, ValueError, subprocess.TimeoutExpired):
                pass
            time.sleep(2)
        raise RuntimeError(what + ' did not become ready')

    def run(self):
        status = self.deployment.snapshot_guard('status')
        self.evidence['files'] = {
            'scripts/backup-snapshot-drill.py': file_hash(HERE / 'backup-snapshot-drill.py'),
            'scripts/rolling-deploy.py': file_hash(HERE / 'rolling-deploy.py'),
            'scripts/backup-zfs-guard.sh': status.get('guard_sha256'),
        }
        before_freeze = time.time()
        snapshot = self.deployment.snapshot_database(self.name)
        self.evidence['snapshot'] = snapshot
        rolling.log(f"DRILL SNAPSHOT: {snapshot['name']} frozen {snapshot['frozen_seconds']}s")

        self.deployment.snapshot_guard('drill-attach', self.name, timeout=90)
        self.attached = True
        self.wait(DEVICE.exists, 60, 'clone device')
        device = str(DEVICE.resolve())
        if sh('blkid', '-s', 'UUID', '-o', 'value', device).strip() != self.config['fs_uuid']:
            raise RuntimeError('attached clone is not a copy of the database filesystem')
        MOUNT.mkdir(mode=0o700, exist_ok=True)
        sh('mount', '-o', 'noatime', device, str(MOUNT))

        sh('docker', 'network', 'create', '--internal', self.network)
        started = time.monotonic()
        sh('docker', 'run', '-d', '--name', MYSQL, '--network', self.network,
           '-v', f'{MOUNT}:/var/lib/mysql', '-v', f'{self.my_cnf}:/etc/my.cnf:ro',
           self.mysql_image, '--innodb-buffer-pool-size=1G')
        self.wait(lambda: self.sql(MYSQL, 'SELECT 1', timeout=10) == [['1']], 900, 'restored MySQL')
        checks = self.evidence['checks']
        checks['mysql_recovery_seconds'] = round(time.monotonic() - started, 1)
        logs = subprocess.run(['docker', 'logs', MYSQL], capture_output=True, text=True, encoding='utf-8', timeout=60)
        checks['mysql_log_errors'] = sum(1 for line in (logs.stdout + logs.stderr).splitlines()
                                         if '[ERROR]' in line or 'orrupt' in line)

        clone_schema = self.sql(MYSQL, SCHEMA_QUERY)
        prod_schema = self.sql(self.config['mysql_container'], SCHEMA_QUERY)
        checks['schema'] = {'columns_and_indexes': len(clone_schema),
                            'equal': clone_schema == prod_schema,
                            'sha256': hashlib.sha256(json.dumps(clone_schema).encode()).hexdigest()}

        top = int(self.sql(MYSQL, 'SELECT COALESCE(MAX(id), 0) FROM logs')[0][0])
        high = max(0, top - TAIL_GAP)
        low = max(0, high - WINDOW)
        window = ("SELECT COUNT(*), COALESCE(BIT_XOR(CRC32(CONCAT_WS('#', id, user_id, created_at, type, quota, "
                  "prompt_tokens, completion_tokens, model_name, token_id, channel_id, request_id))), 0) "
                  f"FROM logs WHERE id > {low} AND id <= {high}; SELECT COUNT(*) FROM logs WHERE id <= {high}")
        clone_logs, prod_logs = self.sql(MYSQL, window), self.sql(self.config['mysql_container'], window)
        checks['logs'] = {'max_id': top, 'window': [low, high], 'clone': clone_logs, 'production': prod_logs,
                          'equal': clone_logs == prod_logs}

        tables = self.sql(self.config['mysql_container'],
                          "SELECT TABLE_NAME, IFNULL(UNIX_TIMESTAMP(UPDATE_TIME), 0), IFNULL(TABLE_ROWS, 0) "
                          "FROM information_schema.TABLES WHERE TABLE_SCHEMA = 'new_api' AND TABLE_TYPE = 'BASE TABLE'")
        static, changed, mismatched = [], [], []
        for name, updated, rows in tables:
            if name == 'logs' or int(rows) > STATIC_ROW_LIMIT:
                continue
            # Changed since the freeze began: production legitimately differs.
            if float(updated) >= before_freeze - 1:
                changed.append(name)
                continue
            query = f'CHECKSUM TABLE `{name}`'
            if self.sql(MYSQL, query)[0][1] != self.sql(self.config['mysql_container'], query)[0][1]:
                mismatched.append(name)
            static.append(name)
        checks['static_tables'] = {'compared': len(static), 'changed_after_snapshot': changed, 'mismatched': mismatched}

        # The official MySQL image ships no mysqlcheck; CHECK TABLE is the same check.
        names = [row[0] for row in self.sql(MYSQL, "SELECT TABLE_NAME FROM information_schema.TABLES "
                                                   "WHERE TABLE_SCHEMA = 'new_api' AND TABLE_TYPE = 'BASE TABLE'")]
        rows = self.sql(MYSQL, 'CHECK TABLE ' + ', '.join(f'`{name}`' for name in names), timeout=3600)
        status = {row[0]: row[3] for row in rows if len(row) == 4 and row[2] == 'status'}
        failed = [row for row in rows if len(row) == 4 and row[2] == 'error']
        failed += [name for name in names if status.get(DATABASE + '.' + name.lower(), status.get(DATABASE + '.' + name)) != 'OK']
        checks['check_table'] = {'tables': len(names), 'ok': sum(v == 'OK' for v in status.values()), 'failed': failed}
        results = names

        checks['application'] = self.application()
        passed = (checks['mysql_log_errors'] == 0 and checks['schema']['equal'] and checks['logs']['equal']
                  and not mismatched and len(static) > 0 and results and not failed
                  and all(checks['application'].values()))
        self.evidence['result'] = 'passed' if passed else 'failed'

    def application(self):
        compose = json.loads(self.deployment.compose_path.read_text(encoding='utf-8'))
        service = compose['services'][self.deployment.state['active']]
        env = dict(service['environment'])
        for key in ('SQL_DSN', 'LOG_SQL_DSN'):
            if env.get(key):
                env[key] = env[key].replace('@tcp(' + self.config['mysql_container'] + ':', '@tcp(' + MYSQL + ':')
        # Rolling roles require Redis; never share production's caches or locks.
        redis_image = self.deployment.inspect('1Panel-new-api-redis')['Image']
        sh('docker', 'run', '-d', '--name', REDIS, '--network', self.network, redis_image)
        env['REDIS_CONN_STRING'] = f'redis://{REDIS}:6379/0'
        env['NODE_NAME'] = WEB
        env_file = self.private / 'web.env'
        env_file.write_text(''.join(f'{k}={v}\n' for k, v in env.items()), encoding='utf-8')
        os.chmod(env_file, 0o600)
        sh('docker', 'run', '-d', '--name', WEB, '--network', self.network, '--env-file', str(env_file),
           '--tmpfs', '/data', '--tmpfs', '/app/logs', service['image'], *service.get('command', []))
        result = {}

        def api(path):
            out = sh('docker', 'exec', WEB, 'wget', '-q', '-T', '10', '-O', '-', 'http://127.0.0.1:3000/api/' + path)
            return json.loads(out).get('success') is True

        def ready():
            out = sh('docker', 'exec', WEB, 'wget', '-q', '-T', '5', '-O', '-', 'http://127.0.0.1:3001/ready')
            return json.loads(out).get('ready') is True

        try:
            self.wait(ready, 180, 'restored application')
        except RuntimeError:
            self.keep_web_log()
            raise
        result['status'], result['pricing'] = api('status'), api('pricing')
        sh('docker', 'restart', '-t', '30', WEB)
        self.wait(ready, 180, 'restarted application')
        result['restart_status'], result['restart_pricing'] = api('status'), api('pricing')
        return result

    def keep_web_log(self):
        # Private (0600) next to the evidence; never printed or embedded in it.
        logs = subprocess.run(['docker', 'logs', '--tail', '200', WEB], capture_output=True, timeout=60)
        path = self.deployment.directory / f'backup-snapshot-{self.name}-web.log'
        with open(path, 'wb') as output:
            os.chmod(path, 0o600)
            output.write(logs.stdout + logs.stderr)
        self.evidence['web_log'] = path.name

    def cleanup(self):
        steps = self.evidence['cleanup']
        for name in (WEB, REDIS, MYSQL):
            steps[name] = subprocess.run(['docker', 'rm', '-f', name], capture_output=True).returncode in (0, 1)
        steps['network'] = subprocess.run(['docker', 'network', 'rm', self.network], capture_output=True).returncode in (0, 1)
        if MOUNT.is_mount():
            steps['umount'] = subprocess.run(['umount', str(MOUNT)], capture_output=True).returncode == 0
        try:
            self.deployment.snapshot_guard('drill-detach', self.name, timeout=90)
            steps['detach'] = True
        except (rolling.DeployError, subprocess.TimeoutExpired) as error:
            steps['detach'] = str(error)
        deadline = time.monotonic() + 30
        while DEVICE.exists() and time.monotonic() < deadline:
            time.sleep(1)
        steps['device_removed'] = not DEVICE.exists()
        for path in self.private.glob('*'):
            path.unlink()
        self.private.rmdir()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', default='/opt/1panel/apps/new-api/new-api')
    args = parser.parse_args()
    os.umask(0o077)
    drill = Drill(args.root)
    try:
        drill.run()
    except Exception as error:  # Evidence must record every failure, then clean up.
        drill.evidence['error'] = f'{type(error).__name__}: {error}'
    finally:
        drill.cleanup()
    evidence = drill.evidence
    if evidence['result'] == 'passed' and not all(v is True for v in evidence['cleanup'].values()):
        evidence['result'] = 'failed'
        evidence['error'] = 'cleanup incomplete'
    evidence['finished'] = time.time()
    path = drill.deployment.directory / f'backup-snapshot-{drill.name}.json'
    rolling.atomic_json(path, evidence)
    print(json.dumps(evidence, ensure_ascii=False))
    raise SystemExit(0 if evidence['result'] == 'passed' else 1)


if __name__ == '__main__':
    main()
