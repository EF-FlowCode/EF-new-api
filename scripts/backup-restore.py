"""Restore a sealed chain into NEW isolated containers; never overwrite a DB.

Requires an independently captured logical-dump hash at the sealed cutoff.
The application must pass readiness and restart before a proof is produced.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
import uuid

from backup_chain import verify_archive, verify_chain, replay_command
from release import run, save
from release_core import ReleaseError, contained, digest

DUMP_ARGS = ['mysqldump', '-uroot', '--compact', '--skip-comments', '--skip-extended-insert',
             '--order-by-primary', '--hex-blob', '--set-gtid-purged=OFF', '--routines', '--events', '--triggers']


def restore(directory, image, expected_hash, evidence, mysql_image='mysql:8.4.8', client_image='new-api-backup-client:8.4.8'):
    manifest = json.loads((directory / 'manifest.json').read_text())
    verify_archive(directory, manifest)
    replay = replay_command(manifest)
    database = manifest['databases'][0]
    if not expected_hash or len(expected_hash) != 64:
        raise ReleaseError('An independent source hash at cutoff is required')
    prefix = 'release-restore-' + uuid.uuid4().hex[:12]
    password = uuid.uuid4().hex
    network, mysql, redis, app = [prefix + '-' + name for name in ('network', 'mysql', 'redis', 'app')]
    resources = []
    report = {'result': 'running', 'server_uuid': manifest['server_uuid'], 'chain_sha256': digest(manifest),
              'cutoff_position': manifest['cutoff_position'], 'production_writes': False}
    evidence.mkdir(parents=True, exist_ok=False)
    env = ['-e', 'MYSQL_PWD=' + password]
    def sql(statement):
        return run(['docker', 'exec', '-i', *env, mysql, 'mysql', '-uroot', '-N', '-B'], input_data=statement.encode())
    def import_file(path):
        with path.open('rb') as stream:
            result = subprocess.run(['docker', 'exec', '-i', *env, mysql, 'mysql', '-uroot'],
                stdin=stream, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=1800)
        if result.returncode:
            raise ReleaseError('Isolated SQL import failed; no production database was touched')
    def ready():
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                for path in ('/api/status', '/api/pricing'):
                    response = run(['docker', 'exec', app, 'wget', '-qO-', 'http://127.0.0.1:3000' + path], timeout=10)
                    if json.loads(response).get('success') is not True:
                        raise ValueError('Application not ready')
                return
            except (ReleaseError, ValueError, OSError):
                time.sleep(1)
        raise ReleaseError('Restored application did not become ready')
    try:
        from release_baseline import validation_networks
        configured = validation_networks({'runtime': {}})['runtime']
        subnet = configured.get('ipam', {}).get('config', [])
        run(['docker', 'network', 'create', '--internal', *(['--subnet', subnet[0]['subnet']] if subnet else []), network])
        for name, argv in (
                (mysql, ['-e', 'MYSQL_ROOT_PASSWORD=' + password, '--mount',
                         'type=bind,source=' + str(directory.resolve()) + ',target=/chain,readonly', mysql_image]),
                (redis, ['redis:7-alpine'])):
            resources.append(name)
            run(['docker', 'run', '-d', '--name', name, '--network', network, *argv])
        deadline = time.monotonic() + 90
        while True:
            try:
                sql('SELECT 1'); break
            except ReleaseError:
                if time.monotonic() > deadline: raise
                time.sleep(1)
        import_file(contained(directory, manifest['snapshot']['file']))
        replay_sql = evidence / 'replay.sql'
        with replay_sql.open('wb') as output:
            result = subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--mount',
                'type=bind,source=' + str(directory.resolve()) + ',target=/chain,readonly',
                client_image, *replay], stdout=output, stderr=subprocess.PIPE, timeout=1800)
        if result.returncode:
            raise ReleaseError('Binlog decoding failed; chain was not restored')
        # SET USE scopes the replay generated for the explicit database.
        with replay_sql.open('rb') as stream:
            result = subprocess.run(['docker', 'exec', '-i', *env, mysql, 'mysql', '-uroot', database],
                stdin=stream, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE, timeout=1800)
        if result.returncode:
            (evidence / 'replay-error.log').write_bytes(result.stderr)
            raise ReleaseError('Binlog replay failed')
        dump = subprocess.run(['docker', 'exec', *env, mysql, *DUMP_ARGS, database], capture_output=True, timeout=300)
        if dump.returncode or hashlib.sha256(dump.stdout).hexdigest() != expected_hash:
            raise ReleaseError('Restored schema/data differs from independently captured cutoff')
        report['schema_and_data_match'] = True
        resources.append(app)
        run(['docker', 'run', '-d', '--name', app, '--network', network,
             '-e', f'SQL_DSN=root:{password}@tcp({mysql}:3306)/{database}?charset=utf8mb4&parseTime=True&loc=Local',
             '-e', 'REDIS_CONN_STRING=redis://' + redis + ':6379', '-e', 'SESSION_SECRET=isolated-backup-restore',
             '-e', 'DEPLOYMENT_ROLE=web', '-e', 'NODE_TYPE=slave', '-e', 'NODE_NAME=' + app, image])
        ready()
        run(['docker', 'restart', app], timeout=120)
        ready()
        report.update(application_restart_passed=True, application_image_id=run(
            ['docker', 'inspect', '--format', '{{.Image}}', app]), verified_at=time.time())
    finally:
        # Preserve generated application audit history before isolated cleanup.
        if app in resources:
            found = subprocess.run(['docker', 'inspect', app], capture_output=True)
            if found.returncode == 0:
                info = json.loads(found.stdout)[0]
                if info['State']['Running']:
                    run(['docker', 'kill', '--signal=TERM', app])
                    run(['docker', 'wait', app], timeout=130)
                from release_local import archive_container_logs
                report['container_logs'] = archive_container_logs(evidence, app)
                audit_dump = subprocess.run(['docker', 'exec', *env, mysql, *DUMP_ARGS, database],
                                            capture_output=True, timeout=180)
                if audit_dump.returncode:
                    raise ReleaseError('Cannot preserve restored database audit history; resources retained')
                (evidence / 'after-application.sql').write_bytes(audit_dump.stdout)
        # Only exact UUID-named resources created by this invocation are removed.
        for name in reversed(resources):
            if not name.startswith(prefix + '-'):
                raise ReleaseError('Unexpected cleanup target')
            run(['docker', 'rm', '-f', '-v', name])
        run(['docker', 'network', 'rm', network])
        report['isolated_resources_cleaned'] = True
        save(evidence / 'restore-proof.json', report)
    report['result'] = 'passed'
    verify_chain(directory, manifest, report)
    save(evidence / 'restore-proof.json', report)
    return report


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--image', required=True)
    parser.add_argument('--expected-hash', required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(restore(args.directory, args.image, args.expected_hash, args.evidence), indent=2))
    except Exception as error:
        sys.exit('Isolated restore incomplete: ' + type(error).__name__)
