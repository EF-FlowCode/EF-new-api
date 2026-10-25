"""Validated release controller, invoked by the persistent server supervisor."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sys
import time

from release_core import (SCHEMA, ReleaseError, contained, digest, file_hash, validate_compatibility_target,
                          validate_worker_impact_target)

WORKER_FINGERPRINT_LABEL = 'cc.e-flowcode.application-fingerprint'
# Metadata that a maintenance cancelled before migration leaves behind.
RETRY_METADATA = ('release_operation', 'last_maintenance', 'previous')

spec = importlib.util.spec_from_file_location('rolling', Path(__file__).with_name('rolling-deploy.py'))
rolling = importlib.util.module_from_spec(spec)
spec.loader.exec_module(rolling)


def check_manifest(directory):
    manifest = json.loads((directory / 'manifest.json').read_text(encoding='utf-8'))
    if manifest.get('schema') != SCHEMA or manifest['plan'].get('schema') != SCHEMA:
        raise ReleaseError('Only schema 2 release manifests are supported')
    for name in ('rolling-deploy.py', 'release_remote.py', 'release_core.py', 'rolling-remote.sh',
                 'maintenance.html', 'maintenance-page.js', 'static_assets.py'):
        if name not in manifest['files']:
            raise ReleaseError('Incomplete controller bundle')
    for name, expected in manifest['files'].items():
        if file_hash(contained(directory, name)) != expected:
            raise ReleaseError('Uploaded release file does not match its manifest')
    return manifest


def convert_state(deployment, operation=None, baseline=None):
    state = deployment.state
    if state.get('schema') == SCHEMA:
        if operation:
            deployment.save(release_operation=operation)
        return
    if state.get('schema') not in (None, 1) or state.get('phase') != 'stable':
        raise ReleaseError('State conversion requires a known, stable source state')
    target = deployment.directory / 'state-before-schema2.json'
    if target.exists():
        if json.loads(target.read_text()) != state:
            raise ReleaseError('State conversion backup already belongs to another state')
    else:
        rolling.atomic_json(target, state)
    # No renaming/deletion of maintenance backup directories or job evidence.
    deployment.save(schema=SCHEMA, release_operation=operation, release_revision=baseline)


def prepare(deployment, directory, manifest):
    deployment.require_stable()
    if digest(deployment.state) != manifest['baseline_state_digest']:
        raise ReleaseError('Production changed since planning; generate a new release plan')
    worker = worker_identity(deployment) if manifest['plan']['mode'] == 'blue_green' else None
    if worker:
        validate_compatibility_target(manifest['plan'], deployment.state, worker['image'])
        validate_worker_impact_target(manifest['plan'], deployment.state, worker['fingerprint'])
    required = (directory / manifest['archive']).stat().st_size * 3 if manifest.get('archive') else 0
    if shutil.disk_usage(deployment.root).free < required + 2 * 1024**3:
        raise ReleaseError('Insufficient space for image preparation')
    image = manifest['candidate_image_id']
    if manifest.get('archive'):
        loaded = deployment.load_image(directory / manifest['archive'], manifest['image'])
        if loaded != image:
            raise ReleaseError('Loaded image differs from locally validated candidate')
    else:
        loaded = json.loads(rolling.run('docker', 'image', 'inspect', image))[0]['Id']
        if loaded != image:
            raise ReleaseError('Component release must retain the verified application')
    if manifest['plan'].get('backup_strategy') == 'zfs_snapshot':
        # Surface a missing guard, key or disk while traffic is still online.
        deployment.check_snapshot_target()
    # Back up state only at activation, so preparing never changes live routing.
    rolling.atomic_json(directory / 'prepared.json', {'schema': SCHEMA, 'result': 'passed',
        'manifest_sha256': file_hash(directory / 'manifest.json'), 'image': image,
        'worker_identity': worker, 'baseline_state': deployment.state, 'time': time.time()})
    rolling.log('PREPARED: image loaded, source state checked; production traffic unchanged')


def worker_identity(deployment):
    info = deployment.inspect(deployment.name('worker'))
    if not info or not info['State']['Running']:
        raise ReleaseError('Existing worker must remain running')
    return {'id': info['Id'], 'image': info['Image'], 'started': info['State']['StartedAt'],
            'fingerprint': ((info.get('Config') or {}).get('Labels') or {}).get(WORKER_FINGERPRINT_LABEL)}


def handoff(plan):
    return plan['mode'] == 'blue_green' and plan.get('worker') == 'handoff'


def require_unchanged_worker(deployment, directory, manifest):
    if manifest['plan']['mode'] == 'blue_green' and not handoff(manifest['plan']):
        worker = worker_identity(deployment)
        validate_compatibility_target(manifest['plan'], deployment.state, worker['image'])
        validate_worker_impact_target(manifest['plan'], deployment.state, worker['fingerprint'])
        prepared = json.loads((directory / 'prepared.json').read_text())
        if worker != prepared.get('worker_identity'):
            raise ReleaseError('Blue-green worker identity changed since preparation')


def worker_source_fields(deployment, manifest):
    """Keep the running worker's source revision for later impact analysis."""
    plan = manifest['plan']
    if plan['mode'] == 'maintenance' or handoff(plan):
        return {'worker_revision': plan['revision'], 'worker_fingerprint': plan['fingerprints']['application']}
    if deployment.state.get('worker_revision'):
        return {}
    impact = plan.get('worker_impact') or {}
    if impact.get('worker_revision'):
        return {'worker_revision': impact['worker_revision'], 'worker_fingerprint': impact['worker_fingerprint']}
    # Before the first recorded maintenance: the worker is the baseline build
    # only when its image fingerprint matches the baseline release.
    label = worker_identity(deployment)['fingerprint']
    if label and label == (deployment.state.get('release_fingerprints') or {}).get('application'):
        return {'worker_revision': deployment.state['release_revision'], 'worker_fingerprint': label}
    return {}


def finish(deployment, directory, manifest):
    if handoff(manifest['plan']):
        worker = worker_identity(deployment)
        prepared = json.loads((directory / 'prepared.json').read_text())
        if worker['image'] != manifest['candidate_image_id'] or worker == prepared.get('worker_identity'):
            raise ReleaseError('Worker handoff did not complete')
    else:
        require_unchanged_worker(deployment, directory, manifest)
    deployment.verify_route(deployment.state['active'])
    if manifest['plan']['build']:
        if deployment.inspect(deployment.name(deployment.state['active']))['Image'] != manifest['candidate_image_id']:
            raise ReleaseError('Active candidate identity mismatch')
        worker_replaced = manifest['plan']['mode'] == 'maintenance' or handoff(manifest['plan'])
        if worker_replaced and deployment.inspect(deployment.name('worker'))['Image'] != manifest['candidate_image_id']:
            raise ReleaseError('Worker candidate identity mismatch')
    worker_source = worker_source_fields(deployment, manifest)
    deployment.save(schema=SCHEMA, release_revision=manifest['plan']['revision'],
                    release_fingerprints=manifest['plan']['fingerprints'], last_release_operation=manifest['release_id'],
                    **worker_source)
    rolling.atomic_json(directory / 'receipt.json', {'schema': SCHEMA, 'result': 'passed', 'release_id': manifest['release_id'],
        'candidate_image_id': manifest['candidate_image_id'], 'mode': manifest['plan']['mode'],
        'active': deployment.state['active'], 'pending_cleanup': deployment.state.get('pending_cleanup')})


def cancelled_before_migration(deployment, directory, manifest):
    """True only when this release's maintenance was cancelled before migration.

    The live state must equal the prepared baseline except for the metadata a
    cancellation leaves behind, and neither the active web slot nor the worker
    may run the candidate. Anything else (a completed or interrupted cutover,
    migration intent, other drift) still requires explicit recovery.
    """
    state = deployment.state
    plan = manifest['plan']
    baseline = json.loads((directory / 'prepared.json').read_text()).get('baseline_state')
    if (plan.get('mode') != 'maintenance' or not isinstance(baseline, dict)
            or digest(baseline) != manifest['baseline_state_digest']):
        return False
    if (state.get('release_operation') != manifest['release_id'] or state.get('phase') != 'stable'
            or state.get('migration_started') or state.get('maintenance_context')
            or state.get('release_revision') != baseline.get('release_revision')
            or state.get('release_revision') == plan['revision']
            or state.get('previous') not in (None, baseline.get('previous'))):
        return False
    for key in set(state) | set(baseline):
        if state.get(key) != baseline.get(key) and key not in RETRY_METADATA and not key.startswith('maintenance_'):
            return False
    active = state.get('active')
    if active not in ('blue', 'green'):
        return False
    candidate = manifest['candidate_image_id']
    return all(deployment.inspect(deployment.name(slot))['Image'] != candidate for slot in (active, 'worker'))


def retryable(deployment, directory, manifest):
    """The supervisor may start a fresh job for this id: nothing released or migrated."""
    prepared = json.loads((directory / 'prepared.json').read_text())
    if prepared.get('manifest_sha256') != file_hash(directory / 'manifest.json'):
        return False
    receipt = directory / 'receipt.json'
    if receipt.exists() and json.loads(receipt.read_text()).get('result') == 'passed':
        return False
    return (digest(deployment.state) == manifest['baseline_state_digest']
            or cancelled_before_migration(deployment, directory, manifest))


def activate(deployment, directory, manifest):
    prepared = json.loads((directory / 'prepared.json').read_text())
    if prepared.get('manifest_sha256') != file_hash(directory / 'manifest.json'):
        raise ReleaseError('Prepared release manifest changed')
    receipt = directory / 'receipt.json'
    if receipt.exists():
        result = json.loads(receipt.read_text())
        if result.get('result') == 'passed':
            rolling.log('Already released; observing existing receipt')
            return
    fresh = digest(deployment.state) == manifest['baseline_state_digest']
    if not fresh:
        # A disconnect after successful switch but before receipt is recoverable
        # only when the persistent operation id matches this exact release.
        if deployment.state.get('release_operation') != manifest['release_id']:
            raise ReleaseError('Production state changed; refusing to activate a stale plan')
        if deployment.state.get('phase') != 'stable':
            raise ReleaseError('Release interrupted; inspect and explicitly recover')
        fresh = cancelled_before_migration(deployment, directory, manifest)
        if fresh:
            # Keep the cancelled attempt's state as evidence, then start over.
            rolling.atomic_json(directory / ('state-before-retry-' + time.strftime('%Y%m%d-%H%M%S') + '.json'),
                                deployment.state)
            rolling.log('RETRY: previous attempt was cancelled before migration; restarting from baseline')
        elif deployment.state.get('release_revision') != manifest['plan']['revision']:
            raise ReleaseError('Cutover outcome needs explicit recovery; refusing to repeat migration')
    if fresh:
        require_unchanged_worker(deployment, directory, manifest)
        convert_state(deployment, manifest['release_id'], manifest['plan']['baseline'])
        mode = manifest['plan']['mode']
        if mode == 'maintenance':
            deployment.maintenance(None, manifest['candidate_image_id'],
                                   backup_strategy=manifest['plan'].get('backup_strategy', 'stopped_full'))
        elif mode == 'blue_green':
            deployment.save(previous_release={'revision': deployment.state.get('release_revision'),
                'fingerprints': deployment.state.get('release_fingerprints'),
                'operation': deployment.state.get('last_release_operation'),
                'worker_image': deployment.state.get('worker_image'),
                'worker_revision': deployment.state.get('worker_revision'),
                'worker_fingerprint': deployment.state.get('worker_fingerprint')})
            deployment.deploy(None, manifest['candidate_image_id'])
            if handoff(manifest['plan']):
                try:
                    deployment.handoff_worker(manifest['candidate_image_id'])
                except Exception as error:
                    # handoff_worker already restored the old worker when it
                    # could; put web back too so both stay on one release.
                    if deployment.state.get('phase') == 'stable':
                        deployment.rollback()
                        raise ReleaseError('Worker handoff failed; web rolled back, old release serving: ' + str(error)) from error
                    raise ReleaseError('Worker handoff interrupted; run recover before anything else: ' + str(error)) from error
        elif mode == 'components':
            deployment.ready(deployment.state['active'])
            deployment.ready('worker')
            # Keep the application and gateway process. nginx validates before
            # reload, existing SSE/upgraded connections retain their workers.
            old = (deployment.directory / 'nginx/nginx.conf').read_bytes()
            rolling.atomic_json(directory / 'component-backup.json', {'nginx': old.decode()})
            try:
                deployment.reload(deployment.state['active'])
                deployment.verify_route(deployment.state['active'])
            except Exception:
                (deployment.directory / 'nginx/nginx.conf').write_bytes(old)
                rolling.run('docker', 'exec', deployment.name('gateway'), 'nginx', '-s', 'reload')
                raise
        elif mode != 'validation':
            raise ReleaseError('Unsupported release mode')
    finish(deployment, directory, manifest)


def recover(deployment, directory, manifest):
    if deployment.state.get('release_operation') != manifest['release_id']:
        raise ReleaseError('Recovery release does not own the current operation')
    mode = manifest['plan']['mode']
    if mode == 'components':
        backup = directory / 'component-backup.json'
        if backup.exists():
            previous = json.loads(backup.read_text())['nginx']
            (deployment.directory / 'nginx/nginx.conf').write_text(previous, encoding='utf-8')
            rolling.run('docker', 'exec', deployment.name('gateway'), 'nginx', '-t')
            rolling.run('docker', 'exec', deployment.name('gateway'), 'nginx', '-s', 'reload')
            deployment.verify_route(deployment.state['active'])
        raise ReleaseError('Previous component configuration restored; create a new plan to publish again')
    deployment.recover()
    # Pre-migration cancellation/reverted blue-green is not release success.
    if deployment.inspect(deployment.name(deployment.state['active']))['Image'] != manifest['candidate_image_id']:
        raise ReleaseError('Old service restored; candidate not released. Generate a new plan')
    if handoff(manifest['plan']) and deployment.inspect(deployment.name('worker'))['Image'] != manifest['candidate_image_id']:
        # recover() put the previous worker back; return web too so both run one release.
        deployment.rollback()
        raise ReleaseError('Worker handoff did not complete; web rolled back, old release serving. Generate a new plan')
    finish(deployment, directory, manifest)


def drain_budget(plan):
    """(settlement, grace) seconds; plans before the grace field use the default."""
    wait = plan.get('drain_seconds', 120)
    grace = plan.get('drain_grace_seconds', rolling.MAINTENANCE_GRACE_SECONDS)
    for value in (wait, grace):
        if type(value) is not int or not 0 <= value <= 900:
            raise ReleaseError('Invalid drain budget in release plan')
    return wait, grace


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare', 'activate', 'recover', 'retryable'])
    parser.add_argument('--root', required=True)
    parser.add_argument('--release', type=Path, required=True)
    args = parser.parse_args()
    os.umask(0o077)
    directory = args.release.resolve()
    deployment = rolling.Deployment(args.root, wait=120)
    if directory.parent != deployment.root / '.releases':
        raise ReleaseError('Release directory is outside the project')
    manifest = check_manifest(directory)
    deployment.wait, deployment.drain_grace = drain_budget(manifest['plan'])
    os.chdir(deployment.root)
    import fcntl
    with (deployment.directory / 'deploy.lock').open('w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.action == 'prepare':
            prepare(deployment, directory, manifest)
        elif args.action == 'activate':
            activate(deployment, directory, manifest)
        elif args.action == 'retryable':
            # Exit 0 only when the supervisor may start a fresh job for this id.
            if not retryable(deployment, directory, manifest):
                sys.exit(3)
        else:
            recover(deployment, directory, manifest)


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, rolling.DeployError, OSError, ValueError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
