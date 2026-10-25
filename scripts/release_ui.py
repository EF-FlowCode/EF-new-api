"""Host orchestration only: browser, API assertions and SQL checks run in Docker."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import uuid

from release import ROOT, PRIVATE, read, run, save
from release_baseline import candidate_roles
from release_core import ReleaseError, file_hash

IMAGE = 'new-api-release-ui:pw1.58.2'
ARTIFACTS = ('api_and_ui.json', 'ui-saved-prices.png', 'ui-public-pricing.png',
             'ui-maintenance.png', 'ui-failure.txt', 'ui-failure.png')


def validate_target(state, inspect):
    target = state.get('validation_target', {})
    prefix = target.get('prefix', '')
    if (not re.fullmatch(r'newapi-upgrade-[a-f0-9]{12}', prefix)
            or target.get('owner') != state['id']
            or target.get('baseline_generation') != state['baseline_generation']):
        raise ReleaseError('UI requires this release\'s baseline clone')
    objects = {}
    for role in ('web', 'worker', 'mysql', 'gateway'):
        name = prefix + '-' + role
        if role != 'gateway' and target.get(role) != name:
            raise ReleaseError('UI clone role identity mismatch')
        info = inspect(name)
        labels = info['Config'].get('Labels', {})
        if (info['Name'] != '/' + name or not info['State']['Running']
                or labels.get('cc.e-flowcode.validation-owner') != state['id']
                or labels.get('cc.e-flowcode.validation-stack') != prefix):
            raise ReleaseError('UI clone ownership/running state mismatch')
        expected = candidate_roles(state['plan']['mode'], state['candidate_image_id'],
                                   state['baseline_state']['worker_image'],
                                   state['plan'].get('worker') == 'handoff').get(role)
        if role in ('web', 'worker') and info['Image'] != expected:
            raise ReleaseError('UI clone is not running the planned image')
        objects[role] = info
    network = prefix + '_runtime'
    if not all(network in info['NetworkSettings']['Networks'] for info in objects.values()):
        raise ReleaseError('UI clone internal network mismatch')
    password = next((value.split('=', 1)[1] for value in objects['mysql']['Config']['Env']
                     if value.startswith('MYSQL_ROOT_PASSWORD=')), None)
    if not password:
        raise ReleaseError('UI clone MySQL credentials unavailable')
    return {'origin': 'http://' + prefix + '-gateway:3000', 'network': network,
            'mysql': {'host': target['mysql'], 'user': 'root', 'password': password, 'database': 'new_api'}}


def main(directory, task_preflight=False):
    directory = Path(directory).resolve()
    if not directory.is_relative_to(PRIVATE.resolve()):
        raise ReleaseError('UI evidence must remain inside the private release directory')
    state = read(directory / 'state.json')
    if directory.name != state['id']:
        raise ReleaseError('UI release directory identity mismatch')
    image = json.loads(run(['docker', 'image', 'inspect', IMAGE]))[0]
    expected = file_hash(ROOT / 'scripts/release-ui.Dockerfile')
    if image['Config'].get('Labels', {}).get('cc.e-flowcode.ui-tools') != expected:
        raise ReleaseError('UI tools missing/outdated; build with scripts/docker-check.sh ui-image after authorization')
    runtime = validate_target(state, lambda name: json.loads(run(['docker', 'inspect', name]))[0])
    network = json.loads(run(['docker', 'network', 'inspect', runtime['network']]))[0]
    if not network.get('Internal'):
        raise ReleaseError('UI must run on the clone\'s internal network')
    attempt = 'ui-run-' + uuid.uuid4().hex[:12]
    folder = directory / attempt
    folder.mkdir()
    container = 'newapi-' + attempt
    volume = container + '-evidence'
    # Kept private for diagnosis; never printed or passed as process arguments.
    save(folder / 'ui-input.json', runtime)
    save(folder / 'state.json', state)
    started = time.monotonic()
    run(['docker', 'volume', 'create', volume])
    run(['docker', 'create', '--name', container, '--network', runtime['network'], '--shm-size', '1g',
         '--label', 'cc.e-flowcode.validation-owner=' + state['id'],
         '-v', 'newapi-src:/src:ro', '-v', volume + ':/evidence',
         '-e', 'RELEASE_DIR=/evidence', '-e', 'RELEASE_IMAGE_ID=' + state['candidate_image_id'],
         image['Id'], 'node', '/src/scripts/' + ('task-pricing-preflight.cjs' if task_preflight else 'release-ui.cjs')])
    for name in ('state.json', 'ui-input.json'):
        run(['docker', 'cp', str(folder / name), container + ':/evidence/' + name])
    result = None
    mock = None
    restart_container = None
    try:
        if task_preflight:
            mock = container + '-upstream'
            run(['docker', 'run', '-d', '--name', mock, '--network', runtime['network'],
                 '--network-alias', 'upstream', '--label', 'cc.e-flowcode.validation-owner=' + state['id'],
                 '-e', 'TASK_HTTP_AUTO_COMPLETE=true', '-v', 'newapi-src:/src:ro', image['Id'],
                 'node', '/src/scripts/task-http-upstream.cjs'])
        # Preserve stdout/stderr; do not auto-remove the container or its volume.
        with (folder / 'container.log').open('xb') as output:
            result = subprocess.run(['docker', 'start', '-a', container], stdout=output,
                                    stderr=subprocess.STDOUT, timeout=1500 if task_preflight else 240)
        if task_preflight and result.returncode == 0:
            # Restart the very same candidate and clone. The second tool run
            # must verify the saved options and repeat the migration CAS.
            from release_baseline import Stack
            stack_dir = directory / 'clone'
            stack = Stack(stack_dir, read(stack_dir / 'identity.json'))
            stack.compose('restart', 'web', 'worker', timeout=360)
            stack.ready(expected_contract=state['plan']['target_contract'])
            restart_container = container + '-restart'
            run(['docker', 'create', '--name', restart_container, '--network', runtime['network'],
                 '--label', 'cc.e-flowcode.validation-owner=' + state['id'],
                 '-v', 'newapi-src:/src:ro', '-v', volume + ':/evidence',
                 '-e', 'RELEASE_DIR=/evidence', '-e', 'TASK_PRICING_RESTART=true', image['Id'],
                 'node', '/src/scripts/task-pricing-preflight.cjs'])
            with (folder / 'restart.log').open('xb') as output:
                result = subprocess.run(['docker', 'start', '-a', restart_container], stdout=output,
                                        stderr=subprocess.STDOUT, timeout=120)
    finally:
        for name in (restart_container, mock):
            if name:
                run(['docker', 'stop', '--time', '15', name])
        info = json.loads(run(['docker', 'inspect', container]))[0]
        if info['State']['Running']:
            run(['docker', 'stop', '--time', '30', container])
        run(['docker', 'cp', container + ':/evidence/.', str(folder)])
        for name in (('task_pricing_readiness.json',) if task_preflight else ARTIFACTS):
            source = folder / name
            if source.exists():
                destination = directory / name
                # New releases have no prior UI evidence; never overwrite an old attempt.
                with destination.open('xb') as output:
                    output.write(source.read_bytes())
        save(folder / 'runner.json', {'container': container, 'volume': volume,
            'restart_container': restart_container, 'mock_provider': mock,
            'image_id': image['Id'], 'network': runtime['network'], 'release_id': state['id'],
            'candidate_image_id': state['candidate_image_id'], 'seconds': round(time.monotonic() - started, 2),
            'exit_code': info['State']['ExitCode'] if not info['State']['Running'] else None})
    if result.returncode or info['State']['ExitCode']:
        raise ReleaseError(('Task pricing readiness blocked/failed' if task_preflight else 'Docker UI check failed') +
                           '; inspect ' + str(folder / 'container.log'))
    proof_name = 'task_pricing_readiness.json' if task_preflight else 'api_and_ui.json'
    proof = read(directory / proof_name)
    if proof.get('result') != 'passed' or proof.get('candidate_image_id') != state['candidate_image_id']:
        raise ReleaseError('Docker UI did not produce matching candidate evidence')
    proof['runner'] = {'file': attempt + '/runner.json', 'sha256': file_hash(folder / 'runner.json'),
                       'image_id': image['Id'], 'execution': 'docker'}
    save(directory / proof_name, proof)
    print('Docker check passed; evidence: ' + str(directory / proof_name), flush=True)


if __name__ == '__main__':
    try:
        main(sys.argv[1], task_preflight='--task-pricing-preflight' in sys.argv[2:])
    except Exception as error:
        sys.exit(str(error) if isinstance(error, ReleaseError) else
                 'Docker UI runner failed: ' + type(error).__name__ + '; inspect private UI artifacts')
