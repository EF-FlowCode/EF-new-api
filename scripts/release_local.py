"""Upgrade a fresh production-baseline clone; never mutate the development instance."""
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
import urllib.request
import uuid

from release import ROOT, PRIVATE, read, run, save
from release_core import ReleaseError, check_binding, digest, file_hash


def archive_container_logs(directory, container='new-api-dev'):
    info = json.loads(run(['docker', 'inspect', container]))[0]
    if info['State']['Running'] or info['State'].get('Paused') or info['State'].get('Restarting'):
        raise ReleaseError('Container must be stopped before archiving audit logs')
    if info['HostConfig']['LogConfig']['Type'] not in ('json-file', 'local', 'journald'):
        raise ReleaseError('Cannot prove complete audit logs for this Docker logging driver')
    prefix = 'container-logs-' + uuid.uuid4().hex
    files = {}
    # Two complete reads of the stopped container must agree, byte for byte.
    # Keep both captures, including partial output on failure, for diagnosis.
    for suffix in ('', '-verify'):
        names = [prefix + suffix + '.' + stream + '.log' for stream in ('stdout', 'stderr')]
        with (directory / names[0]).open('xb') as stdout, (directory / names[1]).open('xb') as stderr:
            result = subprocess.run(['docker', 'logs', '--timestamps', info['Id']],
                                    stdout=stdout, stderr=stderr, timeout=300)
        if result.returncode:
            raise ReleaseError('Container audit log archive failed; partial output retained')
        files.update({name: file_hash(directory / name) for name in names})
    for stream in ('stdout', 'stderr'):
        if files[prefix + '.' + stream + '.log'] != files[prefix + '-verify.' + stream + '.log']:
            raise ReleaseError('Container audit logs changed during archive verification')
    archive = {'container_id': info['Id'], 'image_id': info['Image'],
               'finished_at': info['State']['FinishedAt'],
               'logging_driver': info['HostConfig']['LogConfig']['Type'], 'files': files}
    verify_container_logs(directory, archive, container)
    return archive


def verify_container_logs(directory, archive, container='new-api-dev'):
    from release_core import contained
    info = json.loads(run(['docker', 'inspect', container]))[0]
    if (info['Id'] != archive['container_id'] or info['Image'] != archive['image_id']
            or info['State']['Running'] or info['State'].get('Restarting')
            or info['State']['FinishedAt'] != archive['finished_at']):
        raise ReleaseError('Container changed after audit log archive; refusing recreation')
    if len(archive.get('files', {})) != 4 or any(
            file_hash(contained(directory, name)) != expected for name, expected in archive['files'].items()):
        raise ReleaseError('Container audit log archive is missing or changed')


# eflow: one-time data migrations that legitimately rewrite protected tables.
# Each entry applies only while its provenance option is absent from the stopped
# baseline clone; afterwards protected tables must again be byte-for-byte equal.
CHANNEL_TYPE_RENUMBER = {'58': '1001', '59': '1002', '60': '1003', '61': '1004'}
DECLARED_MIGRATIONS = {
    'ChannelTypeRenumberV1': {'channels': ('type', CHANNEL_TYPE_RENUMBER),
                              'tasks': ('platform', CHANNEL_TYPE_RENUMBER)},
}


def declared_migration_expectations(stack):
    expected = {}
    for option, targets in DECLARED_MIGRATIONS.items():
        applied = stack.sql("SELECT COUNT(*) FROM options WHERE `key` = '" + option + "'").strip()
        if applied != '0':
            continue
        for table, (column, mapping) in targets.items():
            if table in expected:
                raise ReleaseError('Overlapping declared migrations for ' + table)
            expected[table] = stack.remapped_fingerprint(table, column, mapping)
    return expected


def main(directory):
    from release_baseline import Stack, verify_files, candidate_roles
    state = read(directory / 'state.json')
    candidate = state['candidate_image_id']
    baseline = read(directory / 'baseline.json')
    worker_candidate = candidate_roles(state['plan']['mode'], candidate, baseline['production']['worker_image'],
                                       state['plan'].get('worker') == 'handoff')['worker']
    generation = PRIVATE / 'baselines' / state['baseline_generation']
    verify_files(generation, baseline)
    if file_hash(generation / 'manifest.json') != state['baseline_manifest_sha256']:
        raise ReleaseError('Baseline manifest changed')
    stack_dir = directory / 'clone'
    progress_path = directory / 'local-progress.json'
    progress = read(progress_path) if progress_path.exists() else {}
    if progress.get('phase') == 'complete':
        stack = Stack(stack_dir, read(stack_dir / 'identity.json'))
        if stack.inspect(stack.spec['web'])['Image'] != candidate:
            raise ReleaseError('Validated clone image changed')
        stack.ready(expected_contract=state['plan']['target_contract'])
        return
    if progress:
        raise ReleaseError('Incomplete clone retained; inspect it before creating a new validation')
    prefix = 'newapi-upgrade-' + uuid.uuid4().hex[:12]
    stack = Stack.create(stack_dir, prefix, baseline['production']['web_image'],
        baseline['production']['worker_image'], baseline['production']['contract'], state['id'],
        subnet_pool=state.get('validation_subnet_pool'))
    save(progress_path, {'phase': 'cloning', 'baseline_generation': baseline['id']})
    stack.start_database()
    stack.sql((generation / 'database.sql').read_bytes())
    if stack.fingerprint() != baseline['fingerprint']:
        raise ReleaseError('Clone does not match frozen baseline data')
    stack.compose('create', 'web', 'worker')
    with (generation / 'data.tar.gz').open('rb') as source:
        result = subprocess.run(['docker', 'run', '--rm', '-i', '--network', 'none',
            '--volumes-from', stack.spec['web'], '--entrypoint', 'tar', baseline['production']['web_image'],
            '-xzf', '-', '-C', '/data'], stdin=source, capture_output=True, timeout=120)
    if result.returncode:
        raise ReleaseError('Baseline data archive restore failed')
    stack.compose('up', '-d', 'worker', 'web', 'gateway')
    previous_version = stack.ready()
    state['validation_target'] = {k: stack.spec[k] for k in ('prefix', 'web', 'worker', 'mysql', 'origin', 'owner')}
    state['validation_target']['baseline_generation'] = baseline['id']
    save(directory / 'state.json', state)
    def check(name, operation):
        started = time.monotonic()
        state.setdefault('checks', {})[name] = {'result': 'running', 'started': time.time()}
        save(directory / 'state.json', state)
        try:
            data = operation()
            proof = dict(data, result='passed', candidate_image_id=candidate,
                baseline_generation=baseline['id'], baseline_manifest_sha256=state['baseline_manifest_sha256'],
                container=stack.spec['web'], clone_owner=state['id'],
                input_fingerprint=check_binding(name, state['plan']))
            save(directory / (name + '.json'), proof)
        except BaseException:
            state['checks'][name].update(result='failed', seconds=round(time.monotonic()-started, 2))
            save(directory / 'state.json', state)
            raise
        state['checks'][name].update(result='passed', seconds=round(time.monotonic()-started, 2))
        save(directory / 'state.json', state)
        print('验收通过：' + name + ' / ' + str(state['checks'][name]['seconds']) + 's', flush=True)
    context = {}
    def backup():
        stack.compose('stop', 'web', 'worker', timeout=160)
        for role in ('web', 'worker'):
            if stack.inspect(stack.spec[role])['State']['ExitCode'] != 0:
                raise ReleaseError('Baseline clone did not stop cleanly')
        context['before'] = stack.fingerprint()
        context['schema_before'] = stack.schema()
        context['declared'] = declared_migration_expectations(stack)
        context['archives'] = stack.archive_logs(directory)
        stack.dump(directory / 'database.sql')
        stack.data_archive(directory / 'data.tar.gz')
        files = {n: file_hash(directory / n) for n in ('database.sql', 'data.tar.gz')}
        for archive in context['archives'].values():
            files.update(archive['files'])
        save(progress_path, {'phase': 'backed_up', 'baseline_generation': baseline['id']})
        return {'previous_image_id': baseline['production']['web_image'], 'stopped_boundary': True,
                'container_logs': context['archives']['web'], 'worker_logs': context['archives']['worker'], 'files': files}
    def upgrade():
        for role, archive in context['archives'].items():
            verify_container_logs(directory, archive, stack.spec[role])
        config = read(stack.path)
        config['services']['web']['image'] = candidate
        config['services']['worker']['image'] = worker_candidate
        # Migrations always come from the candidate, including the blue-green dry run.
        config['services']['migrate']['image'] = candidate
        save(stack.path, config)
        save(progress_path, {'phase': 'migrating', 'baseline_generation': baseline['id']})
        migration = prefix + '-migration'
        if state['plan']['mode'] == 'maintenance':
            with (directory / 'migration.log').open('ab') as output:
                result = subprocess.run(['docker', 'compose', '-f', str(stack.path), 'run', '--no-deps',
                    '--name', migration, 'migrate'], stdout=output, stderr=subprocess.STDOUT, timeout=600)
            if result.returncode:
                raise ReleaseError('Clone migration failed; original baseline, clone and diagnostics retained')
        elif state['plan'].get('backend_changed'):
            # Blue-green never runs migrations. Prove the candidate needs none:
            # its migrate role must leave every table's structure and rows intact.
            with (directory / 'migration-dry-run.log').open('ab') as output:
                result = subprocess.run(['docker', 'compose', '-f', str(stack.path), 'run', '--rm', '--no-deps',
                    '--name', migration, 'migrate'], stdout=output, stderr=subprocess.STDOUT, timeout=600)
            if result.returncode:
                raise ReleaseError('Candidate migrate role failed on the baseline clone; plan with -Maintenance')
            if stack.schema() != context['schema_before'] or stack.fingerprint() != context['before']:
                raise ReleaseError('Candidate changes database schema or data during migration; '
                                   'blue-green cannot run migrations, plan again with -Maintenance')
            context['migration_free'] = True
        stack.compose('up', '-d', '--no-deps', 'worker', 'web')
        context['version'] = stack.ready(expected_contract=state['plan']['target_contract'])
        for role, image in (('web', candidate), ('worker', worker_candidate)):
            if stack.inspect(stack.spec[role])['Image'] != image:
                raise ReleaseError('Clone role is not running candidate')
        context['after'] = stack.fingerprint()
        declared = context['declared']
        for table in ('users', 'tokens', 'channels', 'logs', 'tasks', 'setups'):
            if context['before'].get(table) == context['after'].get(table):
                continue
            # A declared one-time migration may change a protected table only into the
            # exact expected content; any other difference still fails.
            if table in declared and context['after'].get(table) == declared[table]:
                continue
            raise ReleaseError('Protected representative data changed: ' + table)
        return {'previous_image_id': baseline['production']['web_image'], 'baseline_image_id': baseline['production']['web_image'],
            'previous_version': previous_version, 'candidate_version': context['version'],
            'before': context['before'], 'after': context['after'], 'cloned_from_baseline': True,
            'clone_seed_sha256': baseline['files']['database.sql'], 'worker_image_id': worker_candidate,
            'both_roles_same_candidate': worker_candidate == candidate,
            'declared_migrations': sorted(declared), 'migration_free': context.get('migration_free', False),
            'candidate_contract': state['plan']['target_contract']}
    def restart():
        stack.compose('restart', 'web', 'worker', timeout=160)
        if stack.ready(expected_contract=state['plan']['target_contract']) != context['version'] or stack.fingerprint() != context['after']:
            raise ReleaseError('Clone restart changed data or version')
        verify_files(generation, baseline)
        save(progress_path, {'phase': 'complete', 'baseline_generation': baseline['id'], 'after': context['after']})
        return {'snapshot': context['after'], 'baseline_unchanged': True}
    check('backup', backup)
    check('existing_database_upgrade', upgrade)
    check('restart_idempotence', restart)


if __name__ == '__main__':
    try:
        main(Path(sys.argv[1]).resolve())
    except Exception as error:
        sys.exit('Clone upgrade incomplete: ' + (str(error) if isinstance(error, ReleaseError) else type(error).__name__) +
                 '; inspect retained progress and backups; development and baseline instances are untouched')
