"""Built-in release checks. Real rehearsals are opt-in through release run."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time

from release import ROOT, read, run, save
from release_core import ReleaseError, digest, file_hash


def docker_check_command(action):
    # Windows' system bash is WSL, not Git Bash. Resolve Git's sibling explicitly.
    bash = os.environ.get('NEW_API_BASH')
    if not bash:
        if os.name == 'nt':
            git = shutil.which('git')
            bash = str(Path(git).resolve().parents[1] / 'bin/bash.exe') if git else None
        else:
            bash = shutil.which('bash')
    if not bash or not Path(bash).is_file():
        raise ReleaseError('Git Bash is required; set NEW_API_BASH to its executable')
    return [bash, 'scripts/docker-check.sh', action]


def targeted_tests(changes):
    argv = docker_check_command('release-tests')
    if any(p.startswith('web/') for p in changes):
        argv.append('--web')
    if any(p.endswith('.go') or p in ('go.mod', 'go.sum', 'relaykit/go.mod', 'relaykit/go.sum') for p in changes):
        argv.append('--go')
    # Inherit the private check log, including the first failing Docker command.
    result = subprocess.run(argv, cwd=ROOT, timeout=1800)
    if result.returncode:
        raise ReleaseError('Docker release tests failed; inspect targeted_tests.log')


def main(name, directory):
    state = read(directory / 'state.json')
    if state.get('validation_subnet_pool'):
        os.environ['NEW_API_VALIDATION_SUBNET_POOL'] = state['validation_subnet_pool']
    candidate = state['candidate_image_id']
    def proof(data):
        if data.get('result') != 'passed':
            raise ReleaseError('Check did not pass')
        save(directory / (name + '.json'), dict(data, candidate_image_id=candidate))
    if name == 'targeted_tests':
        targeted_tests(state['plan']['changes'])
        proof({'result': 'passed', 'scope': 'release contracts and deployment state machine'})
    elif name == 'gateway_recovery':
        before = set(directory.glob('gateway-dns-*'))
        run([sys.executable, str(ROOT / 'scripts/rolling-gateway-integration.py'), '--evidence-dir', str(directory)], timeout=300)
        created = set(directory.glob('gateway-dns-*')) - before
        if len(created) != 1:
            raise ReleaseError('Gateway rehearsal output is ambiguous')
        report = read(created.pop() / 'report.json')
        supervisor = directory / ('supervisor-' + str(time.time_ns()))
        run([sys.executable, str(ROOT / 'scripts/release-supervisor-integration.py'), '--evidence', str(supervisor)], timeout=90)
        report['supervisor'] = read(supervisor / 'report.json')
        proof(report)
    elif name == 'blue_green_compatibility':
        run([sys.executable, str(ROOT / 'scripts/bluegreen-integration.py'), str(directory)], timeout=900)
        report = read(directory / 'bluegreen-compatibility-report.json')
        proof(report)
    elif name == 'backup_chain_restore':
        files = ('backup-client.Dockerfile', 'backup-client.repo')
        fingerprint = digest({n: file_hash(ROOT / 'scripts' / n) for n in files})
        image = 'new-api-backup-client:8.4.8'
        found = subprocess.run(['docker', 'image', 'inspect', image], capture_output=True)
        cached = json.loads(found.stdout)[0] if found.returncode == 0 else {}
        if cached.get('Config', {}).get('Labels', {}).get('cc.e-flowcode.backup-client') != fingerprint:
            run(['docker', 'build', '-f', 'scripts/backup-client.Dockerfile', '-t', image,
                 '--label', 'cc.e-flowcode.backup-client=' + fingerprint, 'scripts'], timeout=600)
        output = directory / ('backup-chain-' + str(time.time_ns()))
        run([sys.executable, str(ROOT / 'scripts/backup-integration.py'), '--seed',
             str(directory / 'fixtures/database.sql'), '--image', candidate, '--evidence', str(output)], timeout=600)
        report = read(output / 'report.json')
        report['client_image_id'] = run(['docker', 'image', 'inspect', '--format', '{{.Id}}', image])
        proof(report)
    elif name == 'maintenance_recovery':
        # Fixtures are a current read-only pricing sample plus an existing-local
        # backup. Their source identities are recorded alongside the release.
        fixtures = directory / 'fixtures'
        if not (fixtures / 'pricing.json').exists() or not (fixtures / 'database.sql').exists():
            raise ReleaseError('Current pricing and local database fixtures have not been prepared')
        import bcrypt
        password = os.urandom(24).hex()
        env = dict(os.environ, DRILL_PASSWORD=password,
                   DRILL_PASSWORD_HASH=bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode())
        old = state['baseline_state']
        before = set(directory.glob('pricing-drill-*'))
        run([sys.executable, str(ROOT / 'scripts/maintenance-integration.py'), '--fixtures', str(fixtures),
             '--candidate', state['candidate'], '--old-web', state['old_web_image'],
             '--old-worker', old['worker_image'], '--old-contract', old['contract'],
             '--evidence-dir', str(directory)], env=env, timeout=1800)
        created = set(directory.glob('pricing-drill-*')) - before
        if len(created) != 1:
            raise ReleaseError('Maintenance rehearsal output is ambiguous')
        drill = created.pop()
        save(directory / 'rehearsal.json', {'directory': str(drill)})
        proof(read(drill / 'report.json'))
    elif name in ('billing_equivalence', 'all_model_pricing'):
        output = directory / 'billing-report.json'
        if not output.exists():
            import bcrypt
            password = os.urandom(24).hex()
            env = dict(os.environ, DRILL_PASSWORD=password,
                DRILL_PASSWORD_HASH=bcrypt.hashpw(password.encode(), bcrypt.gensalt()).decode(),
                DRILL_CANDIDATE_IMAGE=state['candidate'], DRILL_EVIDENCE_DIR=str(directory),
                DRILL_OLD_WEB=state['old_web_image'], DRILL_OLD_WORKER=state['baseline_state']['worker_image'],
                DRILL_OLD_CONTRACT=state['baseline_state']['contract'])
            drill = Path(read(directory / 'rehearsal.json')['directory'])
            before = set(directory.glob('pricing-drill-*'))
            run([sys.executable, str(ROOT / 'scripts/billing-integration.py'), str(drill / 'baseline.sql')], env=env, timeout=1200)
            created = set(directory.glob('pricing-drill-*')) - before
            if len(created) != 1:
                raise ReleaseError('Billing rehearsal output is ambiguous')
            save(output, read(created.pop() / 'billing-matrix.json'))
        proof(read(output))
    elif name == 'task_pricing_readiness':
        result = subprocess.run(docker_check_command('task-pricing-preflight') + [str(directory)], cwd=ROOT, timeout=1750)
        if result.returncode:
            raise ReleaseError('Task pricing migration cannot be accepted; inspect task_pricing_readiness.json')
    elif name == 'api_and_ui':
        result = subprocess.run(docker_check_command('release-ui') + [str(directory)], cwd=ROOT, timeout=300)
        if result.returncode:
            raise ReleaseError('Docker API/UI check failed; inspect api_and_ui.log and ui-run-*/container.log')
    else:
        raise ReleaseError('Unknown check')


if __name__ == '__main__':
    try:
        main(sys.argv[1], Path(sys.argv[2]).resolve())
    except Exception as error:
        # Inner commands may contain synthetic credentials; omit traceback.
        sys.exit('Release check failed: ' + (str(error) if isinstance(error, ReleaseError) else type(error).__name__))
