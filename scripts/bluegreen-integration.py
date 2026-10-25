"""Candidate-bound blue-green rehearsal; host orchestration, Docker assertions."""
import importlib.util
import json
import os
from pathlib import Path
import secrets
import subprocess
import sys
import time
import bcrypt

REPO = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(REPO / 'scripts'))
from release import Release, read, save
from release_baseline import require_current, verify_files, rehearsal_user_tier, value_sql
from release_core import digest, file_hash

release_directory = Path(sys.argv[1]).resolve()
if release_directory.parent != REPO / '.releases':
    raise RuntimeError('Expected a private release directory')
release = Release(read(REPO / '.releases/config.json'), release_directory.name)
release.assert_inputs()
require_current(release)
state = release.state
generation = REPO / '.releases/baselines' / state['baseline_generation']
baseline = read(generation / 'manifest.json')
verify_files(generation, baseline)
password = secrets.token_hex(20)
os.environ['DRILL_PASSWORD'] = password
os.environ['DRILL_PASSWORD_HASH'] = bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode()
sys.argv = ['maintenance-integration', '--fixtures', str(release.directory / 'fixtures'),
            '--old-web', state['old_web_image'], '--old-worker', state['baseline_state']['worker_image'],
            '--old-contract', state['baseline_state']['contract'], '--candidate', state['candidate_image_id'],
            '--evidence-dir', str(release_directory)]
spec = importlib.util.spec_from_file_location('drill', REPO / 'scripts/maintenance-integration.py')
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)
root, deploy = m.ROOT, m.deployment
print('EVIDENCE ' + str(root), flush=True)
save(release_directory / 'bluegreen-rehearsal.json', {'directory': str(root)})
runner = m.PREFIX + '-probe'
volume = m.PREFIX + '-probe-files'
report = {'result': 'running', 'candidate_image_id': state['candidate_image_id'],
          'old_web_image': state['old_web_image'], 'old_worker_image': state['baseline_state']['worker_image'],
          'baseline_generation': baseline['id'], 'baseline_manifest_sha256': file_hash(generation / 'manifest.json'),
          'production_writes': False, 'deployment_script_sha256': file_hash(REPO / 'scripts/rolling-deploy.py'),
          'harness_sha256': file_hash(Path(__file__)), 'probe_sha256': file_hash(Path(__file__).with_name('bluegreen-probe.cjs'))}
save(root / 'bluegreen-report.json', report)
process = None
log_bridge = state['plan'].get('compatibility', {}).get('id') == 'log-metadata-bridge-20261001'
# Same-contract backend releases replace the worker in place after the web switch.
handoff = state['plan'].get('worker') == 'handoff'
report['worker_handoff'] = handoff

def worker_identity():
    info = deploy.inspect(deploy.name('worker'))
    return {'id': info['Id'], 'image': info['Image'], 'started': info['State']['StartedAt'], 'running': info['State']['Running']}

def wait_file(name, seconds=180):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        if process.poll() is not None:
            raise RuntimeError('Docker probe stopped; inspect probe.log')
        result = subprocess.run(['docker', 'exec', runner, 'test', '-f', '/work/' + name], capture_output=True)
        if result.returncode == 0:
            return
        time.sleep(1)
    raise TimeoutError('probe stage ' + name)

def signal(name):
    m.run('docker', 'exec', runner, 'touch', '/work/' + name)

try:
    # The upstream test source lives in a Linux volume, never an NTFS bind mount.
    m.run('docker', 'volume', 'create', volume)
    if log_bridge:
        m.run('docker', 'run', '--rm', '--network', 'none', '-e', 'CGO_ENABLED=0',
              '-v', 'newapi-src:/build', '-v', 'newapi-gomod:/go/pkg/mod',
              '-v', 'newapi-gocache:/root/.cache/go-build', '-v', volume + ':/artifact', '-w', '/build',
              'golang:1.26.10-alpine@sha256:27f2d3b83cfc5803c8078f8e730af0f7c431bf067afaec8f60a36fb763187cba',
              'go', 'build', '-trimpath', '-o', '/artifact/log-migrate', './cmd/log-migrate', timeout=180)
    m.infra['services']['upstream']['volumes'] = [{'type': 'volume', 'source': 'probe-files', 'target': '/work', 'read_only': True}]
    m.infra['services']['upstream']['command'] = ['node', '/work/upstream.cjs']
    m.infra['services']['upstream']['init'] = True
    m.infra['volumes']['probe-files'] = {'external': True, 'name': volume}
    # Copy files before starting the fixture upstream.
    m.run('docker', 'create', '--name', runner, '-v', volume + ':/work', '--network', 'none',
          'new-api-release-ui:pw1.58.2', 'tail', '-f', '/dev/null')
    m.run('docker', 'cp', str(REPO / 'scripts/rolling-test-upstream.cjs'), runner + ':/work/upstream.cjs')
    m.run('docker', 'cp', str(Path(__file__).with_name('bluegreen-probe.cjs')), runner + ':/work/probe.cjs')
    m.rolling.atomic_json(m.infra_path, m.infra)
    m.rolling.atomic_json(deploy.compose_path, m.baseline_config)
    deploy.state = dict(m.baseline_state)
    deploy.save()
    deploy.write_route('blue')
    m.run('docker', 'compose', '-f', str(m.infra_path), 'up', '-d', timeout=180)
    deadline = time.monotonic() + 90
    while True:
        try:
            m.sql('SELECT 1')
            break
        except RuntimeError:
            if time.monotonic() > deadline:
                raise
            time.sleep(1)
    m.run('docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + m.PASSWORD, m.MYSQL, 'mysql', '-uroot', 'drill',
          input_data=(generation / 'database.sql').read_bytes())
    restored = {}
    for table in m.sql('SHOW TABLES').splitlines():
        rows = m.sql('SELECT * FROM `' + table + '`').splitlines()
        restored[table] = {'rows': len(rows), 'sha256': digest(sorted(rows))}
    if restored != baseline['fingerprint']:
        raise RuntimeError('Restored data differs from frozen baseline')
    report['baseline_data_verified'] = True
    deploy.compose('create', 'blue', 'worker')
    m.run('docker', 'run', '--rm', '-i', '--network', 'none', '--volumes-from', deploy.name('blue'),
          '--entrypoint', 'tar', state['old_web_image'], '-xzf', '-', '-C', '/data',
          input_data=(generation / 'data.tar.gz').read_bytes())
    # Freeze a backup before adding synthetic actors or starting any application.
    backup = m.run('docker', 'exec', '-e', 'MYSQL_PWD=' + m.PASSWORD, m.MYSQL, 'mysqldump', '-uroot',
                   '--single-transaction', '--routines', '--triggers', 'drill')
    (root / 'before-bluegreen.sql').write_bytes(backup)
    report['backup_sha256'] = file_hash(root / 'before-bluegreen.sql')
    tier = rehearsal_user_tier(read(release.directory / 'fixtures/pricing.json'), '0_Tools_Media')
    m.sql("INSERT INTO users(id,username,password,display_name,role,status,quota,`group`,aff_code,setting) VALUES (2,'local_drill'," +
          value_sql(os.environ['DRILL_PASSWORD_HASH']) + ",'Local Drill',100,1,10000000," + value_sql(tier) + ",'bluegreen-drill','{}')")
    deploy.compose('up', '-d', '--no-deps', 'worker', 'blue')
    deploy.ready('worker')
    deploy.ready('blue')
    deploy.compose('up', '-d', '--no-deps', 'gateway')
    m.wait_api()
    report['worker_before'] = worker_identity()
    config = {'origin': 'http://' + deploy.name('gateway') + ':3000', 'blue': deploy.name('blue'), 'green': deploy.name('green'),
              'mysql': {'host': m.MYSQL, 'user': 'root', 'password': m.PASSWORD, 'database': 'drill'}, 'password': password,
              'upstream': 'http://upstream:8080', 'candidate_image_id': state['candidate_image_id'], 'log_bridge': log_bridge}
    save(root / 'probe-input.json', config)
    m.run('docker', 'cp', str(root / 'probe-input.json'), runner + ':/work/input.json')
    # Recreate only our unstarted copy helper on the isolated runtime network.
    m.run('docker', 'rm', runner)
    m.run('docker', 'create', '--init', '--name', runner, '-v', volume + ':/work', '--network', m.network,
          'new-api-release-ui:pw1.58.2', 'tail', '-f', '/dev/null')
    m.run('docker', 'start', runner)
    with (root / 'probe.log').open('wb') as output:
        process = subprocess.Popen(['docker', 'exec', '-e', 'NODE_PATH=/opt/release-ui/node_modules', runner,
                                    'node', '/work/probe.cjs'], stdout=output, stderr=subprocess.STDOUT)
        wait_file('ready')
        print('OLD READY; switching via production Deployment.deploy', flush=True)
        deploy.wait = 2
        old_worker_image = report['worker_before']['image']
        deploy.save(previous_release={'revision': 'baseline', 'worker_image': old_worker_image})
        deploy.deploy(None, state['candidate_image_id'])
        if not deploy.running('blue') or deploy.state['pending_cleanup'] != 'blue':
            raise RuntimeError('Old in-flight request was not retained')
        if worker_identity() != report['worker_before']:
            raise RuntimeError('Worker identity changed')
        report['coexistence_worker_unchanged'] = True
        if handoff:
            # The old web still serves the open SSE request; the worker handoff
            # must neither interrupt it nor run two workers at once.
            deploy.wait = 120
            deploy.handoff_worker(state['candidate_image_id'])
            deploy.wait = 2
            after = worker_identity()
            if after['image'] != state['candidate_image_id'] or not after['running'] or after['id'] == report['worker_before']['id']:
                raise RuntimeError('Worker handoff did not start the candidate worker')
            if deploy.state['worker_image'] != state['candidate_image_id'] or deploy.state['phase'] != 'stable':
                raise RuntimeError('Worker handoff state was not committed')
            report['worker_handoff_candidate'] = after
        manifests = [read(p) for p in (deploy.directory / 'nginx/asset-manifests').glob('*.json')]
        if {p['image'] for p in manifests} != {state['old_web_image'], state['candidate_image_id']}:
            raise RuntimeError('Both image asset inventories are required')
        save(root / 'asset-manifests.json', manifests)
        m.run('docker', 'cp', str(root / 'asset-manifests.json'), runner + ':/work/asset-manifests.json')
        report['asset_manifests_sha256'] = file_hash(root / 'asset-manifests.json')
        signal('switched')
        wait_file('validated')
        deploy.wait = 25
        deploy.cleanup()
        report['old_web_drained'] = not deploy.running('blue')
        if not report['old_web_drained']:
            raise RuntimeError('Old web failed to drain')
        signal('drained')
        wait_file('ready-rollback')
        deploy.wait = 120
        deploy.rollback()
        deploy.wait = 25
        report['candidate_web_drained_on_rollback'] = not deploy.running('green')
        if handoff:
            restored = worker_identity()
            if restored['image'] != old_worker_image or not restored['running']:
                raise RuntimeError('Rollback did not return the worker to the previous release')
            report['worker_rolled_back'] = restored
        if not report['candidate_web_drained_on_rollback']:
            raise RuntimeError('Candidate web did not drain after rollback')
        signal('rolled-back')
        if log_bridge:
            wait_file('ready-log-migration')
            deploy.deploy(None, state['candidate_image_id'])
            deploy.save(release_revision=state['plan']['revision'])
            deploy.retire_rollback(state['plan']['revision'], state['candidate_image_id'])
            try:
                deploy.rollback()
            except m.rolling.DeployError as error:
                if 'no previous slot' not in str(error):
                    raise
            else:
                raise RuntimeError('Incompatible rollback was not blocked')
            report['incompatible_rollback_retired'] = True
            signal('log-migration-ready')
        if process.wait(timeout=60) != 0:
            raise RuntimeError('Browser/API probe failed; inspect probe.log')
    m.run('docker', 'cp', runner + ':/work/output', str(root / 'browser'))
    probe = read(root / 'browser/report.json')
    if probe['result'] != 'passed':
        raise RuntimeError('Blue-green browser/API assertions did not pass')
    # XLSX content assertions run inside Linux, against the actual browser download.
    m.run('docker', 'run', '--rm', '--network', 'none', '--volumes-from', runner,
          '--entrypoint', 'python3.11', 'new-api-backup-client:8.4.8', '-c',
          "import zipfile,xml.etree.ElementTree as E; "
          "z=zipfile.ZipFile('/work/output/invoices.xlsx'); r=E.fromstring(z.read('xl/worksheets/sheet1.xml')); "
          "ns={'s':'http://schemas.openxmlformats.org/spreadsheetml/2006/main'}; "
          "rows=r.findall('s:sheetData/s:row',ns); assert len(rows)==3; "
          "texts=[''.join(t.itertext()) for t in rows[1:]]; "
          "assert all('001234567890123456789' in t and '123.45' in t for t in texts); "
          "assert any('90011' in t for t in texts) and any('90001' in t for t in texts)")
    report['xlsx_contents_verified'] = True
    report.update(probe=probe, worker_after=worker_identity(), result=probe['result'])
    if log_bridge:
        m.run('docker', 'cp', runner + ':/work/log-migrate', str(root / 'log-migrate'))
        report['migration_tool_sha256'] = file_hash(root / 'log-migrate')
        if not probe.get('log_migration', {}).get('passed'):
            raise RuntimeError('Log migration bridge checks did not pass')
    if handoff:
        if report['worker_after']['image'] != report['worker_before']['image'] or not report['worker_after']['running']:
            raise RuntimeError('Worker is not back on the previous release after rollback')
    elif report['worker_after'] != report['worker_before']:
        raise RuntimeError('Worker changed during cleanup/rollback')
    require_current(release)
    release.assert_inputs()
    report['production_source_rechecked'] = True
    save(root / 'bluegreen-report.json', report)
    save(release_directory / 'bluegreen-compatibility-report.json', report)
    print('RESULT ' + report['result'], flush=True)
except BaseException as error:
    report.update(result='failed', error_type=type(error).__name__, error=str(error))
    save(root / 'bluegreen-report.json', report)
    if process and process.poll() is None:
        signal('abort')
    subprocess.run(['docker', 'cp', runner + ':/work/output', str(root / 'browser')], capture_output=True)
    raise
finally:
    # Keep this independent clone, its volumes and logs for review, but stop
    # it: the rehearsal gateway/upstream ports are fixed, and the next
    # release's drills need them. Never clean another stack.
    for stop in (lambda: deploy.compose('stop', timeout=160),
                 lambda: m.run('docker', 'compose', '-f', str(m.infra_path), 'stop', timeout=120),
                 lambda: m.run('docker', 'stop', runner, timeout=60)):
        try:
            stop()
        except Exception as error:
            print('Could not stop retained resource: ' + type(error).__name__, flush=True)
    print('Retained isolated resources (stopped): ' + m.PREFIX, flush=True)
