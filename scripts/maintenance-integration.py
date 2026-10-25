"""Isolated, opt-in rehearsal of the unchanged production maintenance script.

Uses real Docker/MySQL/Redis/web/worker/nginx and intentionally broken fixtures.
Only the loopback port and readiness timeout differ from Deployment behavior.
Never connects to production. Evidence and database backups remain private.
"""
import argparse
import copy
import hashlib
import http.client
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import tarfile
import threading
import time
import urllib.error
import urllib.request
import uuid
from pricing_equivalence import canonical, compare
from release_baseline import rehearsal_user_tier, value_sql

REPO = Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--fixtures', type=Path, required=True, help='Private directory containing database.sql and pricing.json')
parser.add_argument('--old-contract', default='eflow-rolling-v7')
parser.add_argument('--candidate', default=os.environ.get('DRILL_CANDIDATE_IMAGE', 'new-api-eflow:pricing-v7-20260919-release'))
parser.add_argument('--old-web', default='new-api-eflow:release-20260919-152858-10df2e17')
parser.add_argument('--old-worker', default='new-api-eflow:release-20260918-230500-0e706010-hotfix')
parser.add_argument('--evidence-dir', type=Path, default=Path(tempfile.gettempdir()))
args = parser.parse_args()
FIXTURES = args.fixtures.resolve()
if not os.environ.get('DRILL_PASSWORD') or not os.environ.get('DRILL_PASSWORD_HASH'):
    parser.error('Set matching synthetic DRILL_PASSWORD and bcrypt DRILL_PASSWORD_HASH for the isolated test user')
args.evidence_dir.mkdir(parents=True, exist_ok=True)
spec = importlib.util.spec_from_file_location('rolling', REPO / 'scripts/rolling-deploy.py')
rolling = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rolling)
PREFIX = 'pricing-drill-' + uuid.uuid4().hex[:8]
rolling.PREFIX = PREFIX
ROOT = Path(tempfile.mkdtemp(prefix=PREFIX + '-', dir=args.evidence_dir))
ORIGIN = 'http://127.0.0.1:31890'
OLD_WEB = args.old_web
OLD_WORKER = args.old_worker
NEW = args.candidate
PASSWORD = 'IsolatedDrill20260919'
MYSQL = PREFIX + '-mysql'
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
report = {'scope': 'production baseline clone; schema, all pricing and sanitized representative data, not full production history',
          'root': str(ROOT), 'prefix': PREFIX, 'scenarios': [], 'production_writes': False, 'existing_local_instance_modified': False}

def save_report():
    rolling.atomic_json(ROOT / 'report.json', report)

def log(message):
    rolling.log(message)

def run(*args, timeout=120, input_data=None):
    result = subprocess.run(args, input=input_data, capture_output=True, timeout=timeout)
    if result.returncode:
        # All containers and credentials used by this harness are synthetic.
        (ROOT / 'last-command-error.txt').write_bytes(result.stdout + result.stderr)
        raise RuntimeError(f'isolated command failed: {args[0]} exit {result.returncode}; private error retained')
    return result.stdout

def sql(statement):
    return run('docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + PASSWORD, MYSQL, 'mysql', '--default-character-set=utf8mb4',
               '-uroot', '--raw', '--batch', '--skip-column-names', 'drill',
               input_data=statement.encode('utf-8')).decode('utf-8').strip()

def image_id(ref):
    return run('docker', 'image', 'inspect', '--format', '{{.Id}}', ref).decode().strip()

def api(path, body=None, method=None):
    req = urllib.request.Request(ORIGIN + path, data=json.dumps(body).encode() if body is not None else None,
        headers={'Content-Type': 'application/json', 'New-Api-User': '2'}, method=method)
    with opener.open(req, timeout=8) as r:
        data = json.load(r)
        assert data.get('success'), (path, data.get('message'))
        return data

def wait_api():
    deadline = time.monotonic() + 45
    while True:
        try:
            api('/api/status')
            api('/api/pricing')
            return
        except (OSError, ValueError, AssertionError):
            if time.monotonic() > deadline:
                raise
            time.sleep(0.5)

def endpoint_state():
    try:
        with opener.open(ORIGIN + '/api/status', timeout=3) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code
    except OSError:
        return 'unavailable'

class RehearsalDeployment(rolling.Deployment):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        # The forced-cut maintenance probe must reach the isolated gateway.
        self.gateway_origin = ORIGIN

    def compose(self, *args, **kwargs):
        # Maintenance stops old app roles before replacing them. Preserve every
        # stopped container's full audit stream before Compose can recreate it.
        if args and args[0] == 'up':
            from release_local import archive_container_logs
            for slot in ('blue', 'green', 'worker'):
                name = self.name(slot)
                if run('docker', 'ps', '-aq', '--filter', 'name=^/' + name + '$').strip():
                    if not self.inspect(name)['State']['Running']:
                        archive_container_logs(ROOT, name)
        return super().compose(*args, **kwargs)

    def backup_maintenance(self):
        # Quiesce legitimately flushes in-memory performance data. Compare a
        # restored backup with this stopped boundary, not an earlier live view.
        self.backup_before_fingerprint = database_fingerprint()
        return super().backup_maintenance()

    def ready(self, slot, timeout=90, expected_contract=None):
        return super().ready(slot, timeout=min(timeout, 12), expected_contract=expected_contract)

    def verify_route(self, slot, timeout=15):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                with opener.open(ORIGIN + '/api/status', timeout=3) as r:
                    if r.headers.get('X-New-Api-Instance') == self.name(slot):
                        return
            except OSError:
                pass
            time.sleep(0.25)
        raise rolling.DeployError('isolated gateway did not confirm intended instance')

deployment = RehearsalDeployment(ROOT, wait=8)
infra_path = ROOT / 'infra.json'
network = PREFIX + '-network'
env = {'SQL_DSN': f'root:{PASSWORD}@tcp(mysql:3306)/drill?charset=utf8mb4&parseTime=True&loc=Local',
       'REDIS_CONN_STRING': 'redis://redis:6379', 'SESSION_SECRET': 'isolated-rehearsal-session',
       'BATCH_UPDATE_ENABLED': 'false', 'CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED': 'false',
       'SQL_MAX_OPEN_CONNS': '10', 'SQL_MAX_IDLE_CONNS': '3', 'SYNC_FREQUENCY': '1', 'TZ': 'Asia/Shanghai'}
infra = {'name': PREFIX + '-infra', 'services': {
    'mysql': {'image': 'mysql:8.4.8', 'container_name': MYSQL, 'environment': {'MYSQL_ROOT_PASSWORD': PASSWORD, 'MYSQL_DATABASE': 'drill'},
              'networks': {'runtime': {'aliases': ['mysql']}}, 'volumes': ['mysql-data:/var/lib/mysql'],
              'healthcheck': {'test': ['CMD', 'mysqladmin', 'ping', '-h', '127.0.0.1'], 'interval': '2s', 'retries': 60}},
    'redis': {'image': 'redis:7-alpine', 'container_name': PREFIX + '-redis', 'networks': {'runtime': {'aliases': ['redis']}}},
    'upstream': {'image': 'node:20-bookworm', 'container_name': PREFIX + '-upstream', 'command': ['node', '/upstream.cjs'],
                 'networks': {'runtime': {'aliases': ['upstream']}, 'front': {}}, 'ports': ['127.0.0.1:31891:8080'],
                 'volumes': ['check-source:/src:ro']}},
    'networks': {'runtime': {'name': network, 'internal': True}, 'front': {'name': PREFIX + '-front'}}, 'volumes': {'mysql-data': {}}}
from release_baseline import validation_networks
infra['networks'] = validation_networks(infra['networks'])
infra['volumes']['check-source'] = {'external': True, 'name': 'newapi-src'}
infra['services']['upstream']['command'] = ['node', '/src/scripts/rolling-test-upstream.cjs']
services = {}
for slot in ['blue', 'green', 'worker', 'migrate']:
    role = 'web' if slot in ['blue', 'green'] else slot
    services[slot] = {'image': OLD_WEB if role == 'web' else OLD_WORKER, 'container_name': deployment.name(slot),
        'restart': 'no', 'environment': dict(env, DEPLOYMENT_ROLE=role, NODE_TYPE='slave' if role == 'web' else 'master', NODE_NAME=deployment.name(slot)),
        'networks': {'runtime': {'aliases': [deployment.name(slot)]}}, 'volumes': ['app-data:/data']}
services['gateway'] = {'image': 'nginx:1.28-alpine', 'container_name': deployment.name('gateway'), 'restart': 'no',
    'ports': ['127.0.0.1:31890:3000'], 'networks': {'runtime': {'aliases': ['new-api']}, 'front': {}},
    'volumes': [{'type': 'bind', 'source': str(deployment.directory / 'nginx'), 'target': '/etc/nginx', 'read_only': True}]}
baseline_config = {'services': services, 'networks': {'runtime': {'external': True, 'name': network}, 'front': {'external': True, 'name': PREFIX + '-front'}}, 'volumes': {'app-data': {}}}
baseline_state = {'phase': 'stable', 'active': 'blue', 'previous': None, 'pending_cleanup': None,
                  'worker_image': image_id(OLD_WORKER), 'contract': args.old_contract}
baseline_db = None
baseline_prices = None
baseline_fingerprint = None
stream = None

def price_snapshot():
    return {k: json.loads(bytes.fromhex(v).decode()) for k, v in (line.split('\t') for line in sql(
        "SELECT `key`,HEX(`value`) FROM options WHERE `key` IN ('ModelPricing','ModelRatio','CompletionRatio','CacheRatio','CreateCacheRatio','ImageRatio','AudioRatio','AudioCompletionRatio','BatchInputRatio','ModelPrice','billing_setting.billing_mode','billing_setting.billing_expr','GroupRatio','GroupGroupRatio','QuotaPerUnit','UserUsableGroups','group_ratio_setting.group_special_usable_group','AutoGroups','DefaultUseAutoGroup') ORDER BY `key`").splitlines())}

def database_fingerprint():
    tables = sql('SHOW TABLES').splitlines()
    result = {}
    for table in tables:
        rows = sql('SELECT * FROM `' + table + '`').splitlines()
        result[table] = {'rows': len(rows), 'sha256': hashlib.sha256('\n'.join(sorted(rows)).encode()).hexdigest()}
    return result

def restore_baseline(reason, generated_backup=None, expected_fingerprint=None):
    started = time.monotonic()
    deployment.compose('stop', 'gateway', 'blue', 'green', 'worker', timeout=90)
    retain_audit('before-fixture-restore')
    # Verify isolated container identity immediately before restoring only its DB.
    assert deployment.inspect(MYSQL)['Name'] == '/' + MYSQL and MYSQL.startswith('pricing-drill-')
    run('docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + PASSWORD, MYSQL, 'mysql', '-uroot', 'drill', input_data=generated_backup or baseline_db)
    # A single-database dump restores only the tables it contains. Tables the
    # candidate's migration created after the backup (e.g. audit_logs) stay in
    # place, as a real restore over the same database would leave them; audit
    # records must never be dropped. Every table in the backup must match exactly.
    expected = expected_fingerprint or baseline_fingerprint
    actual = database_fingerprint()
    assert {table: actual.get(table) for table in expected} == expected, 'backup restore logical database mismatch'
    tables_outside_backup = sorted(set(actual) - set(expected))
    rolling.atomic_json(deployment.compose_path, baseline_config)
    deployment.state = copy.deepcopy(baseline_state)
    deployment.save()
    deployment.write_route('blue')
    deployment.compose('up', '-d', '--no-deps', 'worker', 'blue')
    deployment.ready('worker', expected_contract=args.old_contract)
    deployment.ready('blue', expected_contract=args.old_contract)
    deployment.compose('up', '-d', '--no-deps', 'gateway')
    deployment.set_maintenance(False)
    deployment.verify_route('blue')
    wait_api()
    assert price_snapshot() == baseline_prices
    return {'method': 'explicit isolated backup + old Compose/state restore', 'seconds': round(time.monotonic()-started, 2),
            'all_table_logical_fingerprints_match_before_start': True, 'tables_outside_backup': tables_outside_backup,
            'prices_restored': True, 'api_http': 200, 'reason': reason}

def recover_after_restart():
    global deployment
    deployment = RehearsalDeployment(ROOT, wait=15)
    deployment.recover()
    wait_api()
    assert deployment.state['phase'] == 'stable'
    return {'result': 'passed', 'method': 'recover using state loaded by new controller', 'http': 200}


def retain_audit(reason):
    """Preserve complete generated audit history before fixture restore/removal."""
    from release_local import archive_container_logs
    folder = ROOT / ('audit-' + reason + '-' + uuid.uuid4().hex[:12])
    folder.mkdir()
    names = run('docker', 'ps', '-a', '--format', '{{.Names}}', '--filter', 'name=' + PREFIX).decode().splitlines()
    archives = {}
    for name in names:
        if not name.startswith(PREFIX + '-'):
            raise RuntimeError('Unexpected rehearsal audit target')
        info = deployment.inspect(name)
        if not info['State']['Running']:
            archives[name] = archive_container_logs(folder, name)
    dump = run('docker', 'exec', '-e', 'MYSQL_PWD=' + PASSWORD, MYSQL, 'mysqldump', '-uroot',
               '--single-transaction', '--quick', '--hex-blob', '--set-gtid-purged=OFF', 'drill')
    (folder / 'database.sql').write_bytes(dump)
    rolling.atomic_json(folder / 'manifest.json', {'reason': reason, 'containers': archives,
        'database_sha256': hashlib.sha256(dump).hexdigest()})

def open_stream(token):
    req = urllib.request.Request(ORIGIN + '/v1/chat/completions', data=json.dumps({'model': 'qwen3.5-omni-flash',
        'messages': [{'role': 'user', 'content': 'local-only'}], 'stream': True, 'stream_options': {'include_usage': True}}).encode(),
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer sk-' + token})
    s = opener.open(req, timeout=90)
    first = s.readline()
    assert b'hello' in first, first
    return s

def release_stream():
    with opener.open('http://127.0.0.1:31891/release', timeout=4) as r:
        r.read()

def main():
    global baseline_db, baseline_prices, baseline_fingerprint, stream, deployment
    for port in [31890, 31891]:
        with socket.socket() as probe:
            probe.bind(('127.0.0.1', port))
    log('ISOLATED_ROOT ' + str(ROOT))
    rolling.atomic_json(infra_path, infra)
    rolling.atomic_json(deployment.compose_path, baseline_config)
    deployment.state = copy.deepcopy(baseline_state)
    deployment.save()
    deployment.write_route('blue')
    try:
        run('docker', 'compose', '-f', str(infra_path), 'up', '-d', timeout=180)
        deadline = time.monotonic() + 90
        while True:
            try:
                sql('SELECT 1')
                break
            except RuntimeError:
                if time.monotonic() > deadline: raise
                time.sleep(1)
        dump = (FIXTURES / 'database.sql').read_bytes()
        run('docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + PASSWORD, MYSQL, 'mysql', '-uroot', 'drill', input_data=dump)
        fixture = json.loads((FIXTURES / 'pricing.json').read_text(encoding='utf-8-sig'))
        for key, value in fixture.items():
            sql("INSERT INTO options (`key`,`value`) VALUES ('" + key + "',CONVERT(0x" + value.encode().hex() + " USING utf8mb4)) ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)")
        tier = rehearsal_user_tier(fixture, '0_Tools_Media')
        sql("INSERT INTO users(id,username,password,display_name,role,status,quota,`group`,aff_code,setting) VALUES (2,'local_drill','" + os.environ['DRILL_PASSWORD_HASH'] + "','Local Drill',100,1,10000000," + value_sql(tier) + ",'drill20260919','{}')")
        deployment.compose('run', '--rm', '--no-deps', 'migrate', timeout=120)
        deployment.compose('up', '-d', '--no-deps', 'worker', 'blue')
        deployment.ready('worker', expected_contract=args.old_contract)
        deployment.ready('blue', expected_contract=args.old_contract)
        deployment.compose('up', '-d', '--no-deps', 'gateway')
        wait_api()
        api('/api/user/login', {'username': 'local_drill', 'password': os.environ['DRILL_PASSWORD']})
        assert '0_Tools_Media' in api('/api/user/self/groups')['data'], 'Old API did not authorize rehearsal group'
        report['rehearsal_identity'] = {'customer_tier': tier, 'token_group': '0_Tools_Media',
                                      'old_api_authorized': True, 'production_options_modified': False}
        api('/api/channel/', {'mode': 'single', 'channel': {'type': 1, 'name': 'isolated-mock', 'key': 'no-external-key',
            'base_url': 'http://upstream:8080', 'models': 'qwen3.5-omni-flash', 'group': '0_Tools_Media', 'status': 1, 'priority': 99999}})
        api('/api/token/', {'name': 'isolated-drill', 'remain_quota': 1000000, 'expired_time': -1, 'group': '0_Tools_Media'})
        token = sql('SELECT `key` FROM tokens WHERE user_id=2 ORDER BY id DESC LIMIT 1')
        report['images'] = {'old_web': image_id(OLD_WEB), 'old_worker': image_id(OLD_WORKER), 'candidate': image_id(NEW)}
        report['fixture_models'] = len(canonical({k:json.loads(v) for k,v in fixture.items()}))
        tag = 'new-api-eflow:release-' + PREFIX
        archive = ROOT / 'candidate.tar'
        run('docker', 'tag', NEW, tag)
        run('docker', 'save', '-o', str(archive), tag, timeout=180)
        log('BASELINE_READY v6 topology; running settlement-timeout fault')
        stream = open_stream(token)
        deployment.wait = 2
        deployment.drain_grace = 1
        cut_attempts = []
        def relay_never_settles():
            # Simulates a request that keeps running after the gateway cut; the
            # real gateway restart is exercised by the forced_cut scenario.
            cut_attempts.append({'flag': (deployment.directory / 'nginx/maintenance.flag').is_file(),
                                 'active': deployment.control('blue', 'status')['active']})
        deployment.cut_gateway_connections = relay_never_settles
        try:
            deployment.maintenance(archive, tag)
            raise AssertionError('settlement timeout did not occur')
        except rolling.DeployError as e:
            scenario = {'name': 'drain_timeout', 'error': str(e), 'phase': deployment.state['phase'],
                        'new_request_http': endpoint_state(), 'forced_cut_attempts': cut_attempts,
                        'simulated': 'relay kept running after the cut'}
        finally:
            del deployment.cut_gateway_connections
        assert 'settlement timed out' in scenario['error'], scenario['error']
        assert len(cut_attempts) == 1 and cut_attempts[0]['flag'] and cut_attempts[0]['active'] >= 1
        assert scenario['phase'] == 'stable' and scenario['new_request_http'] == 200
        assert deployment.control('blue', 'status')['active'] >= 1
        scenario['recover'] = recover_after_restart()
        release_stream()
        assert b'[DONE]' in stream.read()
        stream.close(); stream = None
        wait_api()
        deadline = time.monotonic() + 10
        while sql('SELECT COUNT(*) FROM logs WHERE user_id=2 AND type=2') != '1':
            assert time.monotonic() < deadline
            time.sleep(0.2)
        used = int(sql('SELECT used_quota FROM users WHERE id=2'))
        assert used == int(sql('SELECT SUM(quota) FROM logs WHERE user_id=2 AND type=2')) > 0
        scenario.update(result='passed', automatic_resume_http=200, original_stream_finished=True, charges=1, used_quota=used)
        report['scenarios'].append(scenario); save_report()
        log('DRAIN_TIMEOUT PASSED: settlement timeout after cut resumed old service; stream preserved, exactly one charge')
        # Back up only after the accepted request has fully settled.
        baseline_db = run('docker', 'exec', '-e', 'MYSQL_PWD=' + PASSWORD, MYSQL, 'mysqldump', '-uroot',
                          '--single-transaction', '--routines', '--triggers', 'drill')
        (ROOT / 'baseline.sql').write_bytes(baseline_db)
        baseline_prices = price_snapshot()
        baseline_fingerprint = database_fingerprint()
        rolling.atomic_json(ROOT / 'baseline-table-fingerprints.json', baseline_fingerprint)
        report['backup'] = {'bytes': len(baseline_db), 'sha256': hashlib.sha256(baseline_db).hexdigest(), 'tables': len(baseline_fingerprint)}
        deployment.wait = 15
        deployment.drain_grace = 2
        # Real forced cut: a stream still open after the grace period is closed
        # by restarting the isolated gateway; the old processes keep running and
        # settle, then the injected backup failure resumes them before migration.
        balance_before = int(sql('SELECT quota + used_quota FROM users WHERE id=2'))
        stream = open_stream(token)
        cut = {}
        original_cut = deployment.cut_gateway_connections
        def observed_cut():
            cut['active_before'] = deployment.control('blue', 'status')['active']
            gateway_before = deployment.inspect(deployment.name('gateway'))['State']['StartedAt']
            original_cut()
            cut['gateway_restarted'] = deployment.inspect(deployment.name('gateway'))['State']['StartedAt'] != gateway_before
            cut['flag_kept'] = (deployment.directory / 'nginx/maintenance.flag').is_file()
            cut['new_request_http'] = endpoint_state()
        deployment.cut_gateway_connections = observed_cut
        original_capture = deployment.capture_backup
        def fail_backup(*args, **kw):
            raise rolling.DeployError('injected backup disk failure')
        deployment.capture_backup = fail_backup
        try:
            deployment.maintenance(archive, tag)
            raise AssertionError('backup failure was ignored')
        except rolling.DeployError as e:
            assert 'injected backup disk failure' in str(e), str(e)
            assert deployment.state['phase']=='stable' and endpoint_state()==200
            assert price_snapshot()==baseline_prices
        finally:
            deployment.capture_backup=original_capture
            del deployment.cut_gateway_connections
        try:
            remainder = stream.read()
        except (OSError, http.client.HTTPException):
            remainder = b''
        stream.close(); stream = None
        assert b'[DONE]' not in remainder, 'forced cut left the stream open'
        assert cut.get('active_before', 0) >= 1 and cut['gateway_restarted'] and cut['flag_kept'], cut
        assert cut['new_request_http'] == 503, cut
        settled = deployment.control('blue', 'status')
        assert settled['active'] == 0 and settled['pending'] == 0, settled
        assert int(sql('SELECT quota + used_quota FROM users WHERE id=2')) == balance_before, 'cut request left quota unsettled'
        assert int(sql('SELECT used_quota FROM users WHERE id=2')) == int(sql('SELECT COALESCE(SUM(quota),0) FROM logs WHERE user_id=2 AND type=2'))
        report['scenarios'].append(dict(cut, name='forced_cut', result='passed', settled_before_backup=True,
                                        maintenance_page_after_restart=True, quota_settled=True))
        report['scenarios'].append({'name':'backup_failure','result':'passed','automatic_resume_http':200,'no_migration':True})
        save_report()
        log('FORCED_CUT PASSED: gateway restart closed the stream, maintenance page kept, old process settled quota')
        log('BACKUP_FAILURE PASSED: automatically restored old service before migration')
        # Invalid canonical document is inserted only after the real old processes stop.
        original_migration = deployment.run_maintenance_migration
        def migration_fault():
            sql("INSERT INTO options (`key`,`value`) VALUES ('ModelPricing','{\"version\":999,\"models\":{}}') ON DUPLICATE KEY UPDATE `value`=VALUES(`value`)")
            return original_migration()
        deployment.run_maintenance_migration = migration_fault
        log('MIGRATION_FAILURE: running real migrate with invalid pricing document')
        try:
            deployment.maintenance(archive, tag)
            raise AssertionError('invalid migration accepted')
        except rolling.DeployError as e:
            scenario = {'name': 'migration_failure', 'error': str(e), 'phase': deployment.state['phase'],
                        'api_http': endpoint_state()}
        finally:
            deployment.run_maintenance_migration = original_migration
        assert scenario['phase'] == 'migrating'
        context=deployment.maintenance_directory()
        failed_name=deployment.state['migration_container']
        diagnostic=json.loads((context/(failed_name+'.json')).read_text(encoding='utf-8'))
        assert diagnostic['exit_code']==1 and diagnostic['log']
        assert PASSWORD not in diagnostic['log']
        generated_backup=(context/'sql_dsn.sql').read_bytes()
        generated_backup_fingerprint=deployment.backup_before_fingerprint
        with tarfile.open(context/'data.tar.gz') as tar:
            assert len(tar.getmembers())>1
        if 'ModelPricing' in baseline_prices:
            value = json.dumps(baseline_prices['ModelPricing']).encode().hex()
            sql("UPDATE options SET `value`=CONVERT(0x" + value + " USING utf8mb4) WHERE `key`='ModelPricing'")
        else:
            sql("DELETE FROM options WHERE `key`='ModelPricing'") # Remove only the deliberately injected invalid document.
        scenario['recover']=recover_after_restart()
        assert int(sql("SELECT JSON_LENGTH(`value`,'$.models') FROM options WHERE `key`='ModelPricing'")) == report['fixture_models']
        scenario.update(result='passed', retained_diagnostic=True, forward_recovery=True)
        scenario['generated_backup_restore']=restore_baseline('reset isolated fixture for next independent fault', generated_backup, generated_backup_fingerprint)
        report['scenarios'].append(scenario); save_report()
        log('MIGRATION_FAILURE PASSED: retained error, fixed fixture, recover finished new version; generated backup restore verified')
        config = copy.deepcopy(baseline_config)
        config['services']['blue']['environment']['SQL_DSN'] = 'root:invalid@tcp(127.0.0.1:1)/drill'
        rolling.atomic_json(deployment.compose_path, config)
        log('STARTUP_FAILURE: valid migration then unreachable database for new Web only')
        try:
            deployment.maintenance(archive, tag)
            raise AssertionError('invalid Web startup accepted')
        except rolling.DeployError as e:
            scenario = {'name': 'new_web_start_failure', 'error': str(e), 'phase': deployment.state['phase'],
                        'api_http': endpoint_state(),
                        'canonical_pricing_exists': sql("SELECT COUNT(*) FROM options WHERE `key`='ModelPricing'") == '1'}
        assert scenario['phase'] == 'starting' and scenario['canonical_pricing_exists']
        before_recover=price_snapshot()
        migration_name=deployment.state['migration_container']
        config=json.loads(deployment.compose_path.read_text(encoding='utf-8'))
        config['services']['blue']['environment']['SQL_DSN']=env['SQL_DSN']
        rolling.atomic_json(deployment.compose_path,config)
        scenario['recover']=recover_after_restart()
        assert price_snapshot()==before_recover
        assert deployment.migration_exists(migration_name)
        scenario.update(result='passed', forward_recovery=True, pricing_unchanged=True)
        report['scenarios'].append(scenario); save_report()
        restore_baseline('reset isolated fixture after forward startup recovery')
        log('STARTUP_FAILURE PASSED: fixed dependency then recover served new version without database rollback')
        original_compose=deployment.compose
        def disconnect_after_start(*args, **kw):
            result=original_compose(*args, **kw)
            if args[:3]==('run','-d','--no-deps'):
                raise rolling.DeployError('injected client disconnect after migration container creation')
            return result
        deployment.compose=disconnect_after_start
        try:
            deployment.maintenance(archive,tag)
            raise AssertionError('disconnect not injected')
        except rolling.DeployError:
            migration_name=deployment.state['migration_container']
            assert deployment.migration_exists(migration_name)
        finally:
            deployment.compose=original_compose
        context=deployment.maintenance_directory()
        recover_after_restart()
        assert (context/(migration_name+'.json')).exists()
        assert len(list(context.glob(PREFIX+'-migrate-*.json')))==1
        report['scenarios'].append({'name':'migration_client_disconnect','result':'passed','same_container_observed':True,'http':200})
        save_report()
        restore_baseline('reset isolated fixture after detached migration recovery')
        log('CLIENT_DISCONNECT PASSED: recover observed the original migration, no duplicate migration')
        log('SUCCESS_PATH: upgrading real v6 topology to v7 with current maintenance script')
        started = time.monotonic()
        deployment.maintenance(archive, tag)
        # No login here: the pre-maintenance cookie must still authorize the old tab.
        preserved_identity = api('/api/user/self')
        assert preserved_identity['data']['id'] == 2, 'session continuity lost during maintenance'
        api('/api/user/self/groups')

        wait_api()
        migrated = price_snapshot()
        price_check = compare(baseline_prices, migrated)
        changes, fields = price_check['classification_changes'], price_check['fields']
        assert deployment.control('blue')['contract']==rolling.CONTRACT and deployment.control('worker')['contract']==rolling.CONTRACT
        assert all(deployment.inspect(deployment.name(s))['Image']==image_id(NEW) for s in ['blue','worker'])
        upgrade_seconds=round(time.monotonic()-started,2)
        stream=open_stream(token);release_stream();assert b'[DONE]' in stream.read();stream.close();stream=None
        deadline=time.monotonic()+10
        while sql('SELECT COUNT(*) FROM logs WHERE user_id=2 AND type=2')!='2':
            assert time.monotonic()<deadline
            time.sleep(0.2)
        quotas=sql('SELECT quota FROM logs WHERE user_id=2 AND type=2 ORDER BY id').splitlines()
        assert len(quotas)==2 and quotas[0]==quotas[1]
        gateway_before = deployment.inspect(deployment.name('gateway'))
        restart_checks = []
        def address(slot):
            return deployment.inspect(deployment.name(slot))['NetworkSettings']['Networks'][network]['IPAddress']
        for cycle in range(3):
            before = {slot: address(slot) for slot in ['worker', 'blue']}
            deployment.compose('restart','worker','blue')
            deployment.ready('worker');deployment.ready('blue')
            deployment.verify_route('blue');wait_api()
            assert price_snapshot()==migrated
            restart_checks.append({'cycle': cycle + 1, 'before': before,
                'after': {slot: address(slot) for slot in ['worker', 'blue']}, 'http': 200})
        address_checks = []
        for cycle in range(2):
            # Occupy only this isolated Web's former IP so Docker must assign another.
            holder = PREFIX + '-ip-holder-' + str(cycle)
            old_ip = address('blue')
            run('docker', 'network', 'disconnect', network, deployment.name('blue'))
            try:
                run('docker', 'run', '-d', '--name', holder, '--network', network,
                    '--ip', old_ip, 'alpine:3.22', 'sleep', '120')
                run('docker', 'network', 'connect', '--alias', deployment.name('blue'), network, deployment.name('blue'))
                new_ip = address('blue')
                assert new_ip != old_ip
                started = time.monotonic()
                deployment.ready('blue');deployment.verify_route('blue');wait_api()
                assert price_snapshot()==migrated
                address_checks.append({'before': old_ip, 'after': new_ip, 'http': 200,
                    'recovery_seconds': round(time.monotonic() - started, 2), 'manual_reload': False})
            finally:
                run('docker', 'rm', '-f', holder)
        gateway_after = deployment.inspect(deployment.name('gateway'))
        assert gateway_before['Id'] == gateway_after['Id']
        assert gateway_before['State']['StartedAt'] == gateway_after['State']['StartedAt']
        report['scenarios'].append({'name':'successful_upgrade','result':'passed','seconds':upgrade_seconds,'models':price_check['models'],
            'fields':fields,'classification_changes':changes,'prices_and_groups_preserved':True,'legacy_option_rows':0,
            'both_roles_same_candidate':True,'actual_old_new_request_quota':quotas,'restart_idempotent':True,'api_http':200,
            'restart_checks':restart_checks,'address_checks':address_checks,'gateway_restarted_or_reloaded':False})
        report['result']='passed'
        report['candidate_image_id']=image_id(NEW)
        report['deployment_script_sha256']=hashlib.sha256((REPO/'scripts/rolling-deploy.py').read_bytes()).hexdigest()
        report['safe_for_unattended_production_release']=False # Fault causes still require deliberate repair; this is not an unconditional deployment authorization.
        save_report()
        log('ALL SEVEN SCENARIOS PASSED: normal upgrade, forced cut, pre-migration automatic resume, post-migration forward recovery and client disconnect')
    finally:
        if stream is not None:
            try: release_stream();stream.close()
            except OSError: pass
        # Targets are only the UUID-named projects generated above, never existing dev/prod.
        assert PREFIX.startswith('pricing-drill-') and deployment.root == ROOT.resolve()
        try:
            deployment.compose('stop', 'gateway', 'blue', 'green', 'worker', timeout=160)
            retain_audit('final')
            deployment.compose('down','--volumes','--remove-orphans', timeout=120)
            run('docker','compose','-f',str(infra_path),'down','--volumes',timeout=120)
            report['isolated_resources_cleaned']=True
        finally:
            save_report()
        log('EVIDENCE ' + str(ROOT / 'report.json'))

if __name__ == '__main__':
    main()
