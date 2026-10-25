#!/usr/bin/env python3
"""One release entry: plan -> prepare -> run/resume -> status/recover.

Commands are argv arrays, never shell snippets. Private config and artifacts live
under .releases (ignored). Execution is explicit; `plan` is read-only.
"""
import argparse
import contextlib
import gzip
import json
import os
from pathlib import Path
import re
import shlex
import shutil
import subprocess
import sys
import tarfile
import threading
import time

from release_core import (SCHEMA, ReleaseError, check_binding, contained, digest, domain, file_hash,
                          fingerprints, git, inventory, make_plan, validate_evidence, validate_compatibility_target,
                          validate_worker_impact_target)
import release_impact

ROOT = Path(__file__).resolve().parents[1]
PRIVATE = ROOT / '.releases'
SSH = 'C:/Windows/System32/OpenSSH/ssh.exe' if os.name == 'nt' else 'ssh'
SCP = 'C:/Windows/System32/OpenSSH/scp.exe' if os.name == 'nt' else 'scp'
WORKER_FINGERPRINT_LABEL = 'cc.e-flowcode.application-fingerprint'
REMOTE_FILES = ['rolling-deploy.py', 'release_remote.py', 'release_core.py',
                'rolling-remote.sh', 'maintenance.html', 'maintenance-page.js', 'static_assets.py']


def standard_checks():
    return {name: {'argv': ['{python}', 'scripts/release-checks.py', name, '{release}'],
                   'isolated': name in ('targeted_tests', 'gateway_recovery', 'backup_chain_restore')}
            for name in ('targeted_tests', 'gateway_recovery', 'maintenance_recovery',
                         'all_model_pricing', 'billing_equivalence', 'api_and_ui', 'backup_chain_restore',
                         'blue_green_compatibility', 'task_pricing_readiness')}


def save(path, value):
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temp = path.with_suffix(path.suffix + '.tmp')
    with temp.open('w', encoding='utf-8') as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temp, path)


def read(path):
    return json.loads(Path(path).read_text(encoding='utf-8-sig'))


def docker_list(kind):
    """Names of existing containers, volumes or networks; empty when the daemon is unreachable."""
    args = ['docker', 'ps', '-a', '--format', '{{.Names}}'] if kind == 'container' else \
           ['docker', 'volume', 'ls', '--format', '{{.Name}}'] if kind == 'volume' else \
           ['docker', 'network', 'ls', '--format', '{{.Name}}']
    result = subprocess.run(args, capture_output=True)
    return result.stdout.decode('utf-8').split() if result.returncode == 0 else []


def run(args, *, cwd=ROOT, env=None, log=None, input_data=None, timeout=120):
    env = dict(os.environ if env is None else env, PYTHONUTF8='1', PYTHONIOENCODING='utf-8')
    if log:
        with Path(log).open('ab') as output:
            result = subprocess.run(args, cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                                    stdout=output, stderr=subprocess.STDOUT, timeout=timeout)
    else:
        result = subprocess.run(args, cwd=cwd, env=env, input=input_data,
                                capture_output=True, timeout=timeout)
    if result.returncode:
        if not log and env.get('RELEASE_DIR'):
            directory = Path(env['RELEASE_DIR']).resolve()
            if directory.is_relative_to(PRIVATE.resolve()):
                (directory / ('command-error-' + str(time.time_ns()) + '.log')).write_bytes(result.stdout + result.stderr)
        raise ReleaseError(f'{Path(args[0]).name} failed ({result.returncode}); inspect private release log')
    return '' if log else result.stdout.decode('utf-8').strip()


@contextlib.contextmanager
def release_lock():
    PRIVATE.mkdir(exist_ok=True)
    with (PRIVATE / 'release.lock').open('a+b') as lock:
        lock.seek(0)
        if os.name == 'nt':
            import msvcrt
            try:
                msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
            except OSError:
                raise ReleaseError('Another local release is running') from None
        else:
            import fcntl
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        try:
            yield
        finally:
            if os.name == 'nt':
                lock.seek(0)
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)


class Release:
    def __init__(self, config, release_id=None):
        self.config = config
        self.id = release_id
        if release_id and not re.fullmatch(r'release-[A-Za-z0-9_.-]+', release_id):
            raise ReleaseError('Invalid release id')
        self.directory = PRIVATE / release_id if release_id else None
        self.state = read(self.directory / 'state.json') if self.directory else {}
        self.progress_lock = threading.Lock()

    def persist(self, **fields):
        self.state.update(fields)
        save(self.directory / 'state.json', self.state)

    def ssh(self, args, **kwargs):
        host = self.config['host']
        if not re.fullmatch(r'[A-Za-z0-9_.-]+', host) or host.startswith('-'):
            raise ReleaseError('Invalid SSH alias')
        return run([SSH, '-o', 'BatchMode=yes', host, shlex.join(args)], **kwargs)

    def remote_path(self):
        path = self.config['remote_root']
        if not re.fullmatch(r'/[A-Za-z0-9_./-]+', path) or '..' in path.split('/'):
            raise ReleaseError('Remote path must be an explicit absolute project path')
        return path + '/.releases/' + self.id

    def baseline(self):
        text = self.ssh(['cat', self.config['remote_root'] + '/.rolling/state.json'])
        remote = json.loads(text)
        if remote.get('phase') != 'stable':
            raise ReleaseError('Remote is not stable; observe/recover the existing operation first')
        revision = remote.get('release_revision')
        if not revision:
            bootstrap = self.config.get('bootstrap', {})
            if bootstrap.get('worker_image') != remote.get('worker_image'):
                raise ReleaseError('One-time bootstrap requires the verified deployed image and revision')
            web_image = self.ssh(['docker', 'inspect', '--format', '{{.Image}}', 'new-api-rolling-' + remote['active']])
            if bootstrap.get('web_image') != web_image or bootstrap.get('contract') != remote.get('contract'):
                raise ReleaseError('Bootstrap web image/contract does not match the reviewed production baseline')
            revision = bootstrap['revision']
        return revision, remote

    def worker_label(self):
        return self.ssh(['docker', 'inspect', '--format',
                         '{{index .Config.Labels "' + WORKER_FINGERPRINT_LABEL + '"}}', 'new-api-rolling-worker'])

    def worker_source(self, remote):
        """Source identity of the running worker, or None when it cannot be proven.

        Maintenance releases record it in production state; older states are
        accepted only when the worker image carries the baseline fingerprint.
        """
        label = self.worker_label()
        revision, fingerprint = remote.get('worker_revision'), remote.get('worker_fingerprint')
        if not revision and (remote.get('release_fingerprints') or {}).get('application') == label:
            revision, fingerprint = remote.get('release_revision'), label
        if not revision or not label or fingerprint != label:
            return None
        return {'revision': revision, 'fingerprint': label, 'contract': remote.get('contract'),
                'tool': release_impact.tool_digest(ROOT)}

    def plan(self, baseline, remote, force_maintenance=False, force_handoff=False):
        return make_plan(ROOT, baseline, self.config, worker=self.worker_source(remote),
                         analyze=release_impact.analyze, live_contract=remote.get('contract'),
                         force_maintenance=force_maintenance, force_handoff=force_handoff)

    def create(self, local_only=False, force_maintenance=False, force_handoff=False):
        baseline, remote = self.baseline()
        plan = self.plan(baseline, remote, force_maintenance, force_handoff)
        if plan.get('compatibility'):
            worker = self.ssh(['docker', 'inspect', '--format', '{{.Image}}', 'new-api-rolling-worker'])
            validate_compatibility_target(plan, remote, worker)
        validate_worker_impact_target(plan, remote, self.worker_label())
        if plan['mode'] == 'docs':
            print(json.dumps(plan, ensure_ascii=False, indent=2))
            return False
        # A release tag must describe committed source, not a mutable dirty tree.
        if not local_only and plan['mode'] != 'validation' and git(ROOT, 'status', '--porcelain'):
            raise ReleaseError('Commit the reviewed changes before creating a release; plan remains available')
        self.id = 'release-' + time.strftime('%Y%m%d-%H%M%S') + '-' + plan['id'][:8]
        self.directory = PRIVATE / self.id
        self.directory.mkdir(parents=True, exist_ok=False)
        self.state = {'schema': SCHEMA, 'id': self.id, 'plan': plan, 'baseline_state': remote,
                      'baseline_state_digest': digest(remote), 'stages': {}, 'validation_only': local_only,
                      'config_digest': digest(self.config)}
        self.state['old_web_image'] = self.ssh(['docker', 'inspect', '--format', '{{.Image}}',
                                              'new-api-rolling-' + remote['active']])
        self.persist(phase='planned')
        print('发布计划：' + self.id + ' / ' + plan['mode'], flush=True)
        return True

    def step(self, name, fn):
        if self.state['stages'].get(name, {}).get('result') == 'passed':
            return
        self.state['stages'][name] = {'result': 'running', 'started': time.time()}
        self.persist(phase=name)
        print('发布阶段：' + name, flush=True)
        started = time.monotonic()
        try:
            fn()
        except BaseException as error:
            self.state['stages'][name].update(result='interrupted' if isinstance(error, KeyboardInterrupt) else 'failed',
                seconds=round(time.monotonic() - started, 2), error_type=type(error).__name__)
            self.persist()
            raise
        self.state['stages'][name] = {'result': 'passed', 'seconds': round(time.monotonic() - started, 2)}
        self.persist()

    def assert_inputs(self):
        if self.state.get('config_digest') != digest(self.config):
            raise ReleaseError('Release target or validation configuration changed; create a new plan')
        current = fingerprints(ROOT)
        expected = self.state['plan']['fingerprints']
        if any(current[k] != expected[k] for k in ('application', 'deployment', 'tests')):
            raise ReleaseError('Release inputs changed; create a new plan (cached identical application image can be reused)')

    def tag(self):
        expected = self.state['plan']['revision']
        exists = subprocess.run(['git', 'rev-parse', '--verify', 'refs/tags/' + self.id], cwd=ROOT, capture_output=True)
        if exists.returncode == 0:
            if git(ROOT, 'rev-parse', self.id + '^{commit}') != expected:
                raise ReleaseError('Release tag already points elsewhere')
            return
        run(['git', 'tag', '-a', self.id, expected, '-m', '发布 ' + self.id])

    def build(self):
        plan = self.state['plan']
        if not plan['build']:
            # Components must be validated against the actual existing app.
            candidate = self.state['old_web_image']
            image = run(['docker', 'image', 'inspect', '--format', '{{.Id}}', candidate])
            self.persist(candidate=image, candidate_image_id=image)
            return
        app = plan['fingerprints']['application']
        tag = 'new-api-eflow:release-app-' + app[:24]
        cache = PRIVATE / 'images' / (app + '.json')
        if cache.exists():
            cached = read(cache)
            probe = subprocess.run(['docker', 'image', 'inspect', cached['image_id']], capture_output=True)
            if probe.returncode == 0:
                image = json.loads(probe.stdout)[0]
                if image['Config'].get('Labels', {}).get('cc.e-flowcode.application-fingerprint') != app:
                    raise ReleaseError('Cached image provenance mismatch')
                run(['docker', 'tag', cached['image_id'], tag])
                self.persist(candidate=tag, candidate_image_id=cached['image_id'])
                return
        # Build context and application fingerprint use the same inventory.
        # No tests, release scripts, host caches or documentation can invalidate
        # Docker layers. Tar has normalized metadata for reproducibility.
        context = self.directory / 'context.tar'
        with tarfile.open(context, 'w') as archive:
            for path, checksum in inventory(ROOT)['application'].items():
                if checksum == 'deleted':
                    continue
                info = archive.gettarinfo(str(ROOT / path), arcname=path)
                info.mtime = 0
                info.uid = info.gid = 0
                info.uname = info.gname = ''
                with (ROOT / path).open('rb') as stream:
                    archive.addfile(info, stream)
        with context.open('rb') as stream, (self.directory / 'build.log').open('ab') as output:
            result = subprocess.run(['docker', 'build', '-t', tag, '--build-arg', 'BUILD_VERSION=' + tag.split(':')[1],
                '--build-arg', 'APPLICATION_FINGERPRINT=' + app,
                '--label', 'cc.e-flowcode.application-fingerprint=' + app, '-'], cwd=ROOT, stdin=stream,
                stdout=output, stderr=subprocess.STDOUT)
        if result.returncode:
            raise ReleaseError('Build failed; inspect build.log')
        image = run(['docker', 'image', 'inspect', '--format', '{{.Id}}', tag])
        save(cache, {'image_id': image, 'application': app, 'tag': tag})
        self.persist(candidate=tag, candidate_image_id=image)

    def validate(self):
        plan = self.state['plan']
        # Validate all runner definitions BEFORE touching the local database.
        runners = standard_checks() | self.config.get('checks', {})
        builtin = {'backup', 'existing_database_upgrade', 'restart_idempotence'}
        for name in plan['checks']:
            if name not in builtin and not runners.get(name):
                raise ReleaseError('Missing executable check configuration: ' + name)
        env = dict(os.environ, RELEASE_IMAGE=self.state['candidate'], RELEASE_DIR=str(self.directory),
                   RELEASE_IMAGE_ID=self.state['candidate_image_id'])
        def execute_check(name):
            started = time.monotonic()
            def progress(result):
                with self.progress_lock:
                    self.state.setdefault('checks', {})[name] = {'result': result,
                        'seconds': round(time.monotonic() - started, 2)}
                    self.persist()
            config = runners[name]
            if not isinstance(config.get('argv'), list) or not config['argv']:
                raise ReleaseError('Check command must be an argv array: ' + name)
            argv = [str(x).replace('{python}', sys.executable).replace('{image}', self.state['candidate'])
                    .replace('{release}', str(self.directory)) for x in config['argv']]
            print('开始验收：' + name, flush=True)
            progress('running')
            try:
                run(argv, env=env, log=self.directory / (name + '.log'), timeout=config.get('timeout', 1800))
            except BaseException:
                progress('failed')
                raise
            source = contained(self.directory, config.get('evidence', name + '.json'))
            proof = read(source)
            if proof.get('result') != 'passed' or proof.get('candidate_image_id') != self.state['candidate_image_id']:
                raise ReleaseError('Check did not prove candidate success: ' + name)
            proof['fingerprints'] = {k: v for k, v in plan['fingerprints'].items() if k != 'docs'}
            proof['input_fingerprint'] = check_binding(name, plan)
            proof['baseline_generation'] = self.state['baseline_generation']
            proof['baseline_manifest_sha256'] = self.state['baseline_manifest_sha256']
            save(self.directory / (name + '.json'), proof)
            progress('passed')
            print('验收通过：' + name + ' / ' + str(round(time.monotonic() - started, 2)) + 's', flush=True)
        # Stop at the first failed check. Every verification gets a fresh clone.
        if 'targeted_tests' in plan['checks']:
            execute_check('targeted_tests')
        if builtin.intersection(plan['checks']):
            try:
                run([sys.executable, str(ROOT / 'scripts/release_local.py'), str(self.directory)],
                    env=env, log=self.directory / 'local-upgrade.log', timeout=1200)
            finally:
                self.state = read(self.directory / 'state.json')
        for name in ('task_pricing_readiness', 'gateway_recovery', 'backup_chain_restore', 'maintenance_recovery',
                     'all_model_pricing', 'billing_equivalence', 'api_and_ui', 'blue_green_compatibility'):
            if name in plan['checks']:
                execute_check(name)
        checks = [{'name': n, 'file': n + '.json', 'sha256': file_hash(self.directory / (n + '.json'))}
                  for n in plan['checks']]
        record = {'schema': SCHEMA, 'result': 'passed', 'container': self.state['validation_target']['web'], 'plan_id': plan['id'],
                  'validation_contract': 'baseline-clone-v1', 'release_id': self.id,
                  'baseline_generation': self.state['baseline_generation'],
                  'baseline_manifest_sha256': self.state['baseline_manifest_sha256'],
                  'candidate_image_id': self.state['candidate_image_id'], 'checks': checks}
        if plan['mode'] == 'validation' and not builtin.intersection(plan['checks']):
            record['container'] = 'isolated_checks'
            self.assert_inputs()
            save(self.directory / 'evidence.json', record)
            return
        save(self.directory / 'evidence.json', record)
        self.gate()

    def gate(self):
        from release_baseline import Stack, require_current, candidate_roles
        self.assert_inputs()
        require_current(self)
        spec = read(self.directory / 'clone/identity.json')
        stack = Stack(self.directory / 'clone', spec)
        target = self.state['validation_target']
        if (spec['owner'] != self.id or target['owner'] != self.id
                or spec['web'] != target['web'] or target['baseline_generation'] != self.state['baseline_generation']):
            raise ReleaseError('Validation clone identity changed')
        candidate = self.state['candidate_image_id']
        for role, expected in candidate_roles(self.state['plan']['mode'], candidate, self.state['baseline_state']['worker_image'],
                                         self.state['plan'].get('worker') == 'handoff').items():
            info = stack.inspect(spec[role])
            if info['Image'] != expected or not info['State']['Running']:
                raise ReleaseError('Validation clone is not running the validated candidate')
        validate_evidence(self.state['plan'], read(self.directory / 'evidence.json'), self.directory, candidate, candidate)

    def upload(self):
        if self.state.get('validation_only'):
            raise ReleaseError('Local-only validation cannot upload or switch production')
        self.gate()
        target = self.remote_path()
        self.ssh(['mkdir', '-p', target])
        files = [ROOT / 'scripts' / n for n in REMOTE_FILES]
        manifest = {'schema': SCHEMA, 'release_id': self.id, 'plan': self.state['plan'],
                    'baseline_state_digest': self.state['baseline_state_digest'],
                    'candidate_image_id': self.state['candidate_image_id'],
                    'files': {p.name: file_hash(p) for p in files}}
        if self.state['plan']['build']:
            tag = 'new-api-eflow:' + self.id
            run(['docker', 'tag', self.state['candidate_image_id'], tag])
            raw = self.directory / 'image.tar'
            archive = self.directory / 'image.tar.gz'
            if not archive.exists():
                run(['docker', 'save', '-o', str(raw), tag], timeout=300)
                with raw.open('rb') as source, gzip.open(archive, 'wb', compresslevel=3) as dest:
                    shutil.copyfileobj(source, dest)
                raw.unlink()
            files.append(archive)
            manifest.update(image=tag, archive='image.tar.gz')
            manifest['files']['image.tar.gz'] = file_hash(archive)
        save(self.directory / 'manifest.json', manifest)
        files.append(self.directory / 'manifest.json')
        run([SCP, '-q', *map(str, files), self.config['host'] + ':' + target + '/'], timeout=600)
        self.ssh(['python3', target + '/release_remote.py', 'prepare', '--root', self.config['remote_root'],
                  '--release', target], timeout=600)

    def production_pricing(self):
        # Transfer program over stdin; no script install, DB writes or secret output.
        source = (ROOT / 'scripts/pricing-snapshot.py').read_bytes()
        return json.loads(self.ssh(['python3', '-', self.config['remote_root'], self.config['mysql_container']], input_data=source))

    def fixtures(self):
        for image in {self.state['old_web_image'], self.state['baseline_state']['worker_image']}:
            if not re.fullmatch(r'sha256:[a-f0-9]{64}', image):
                raise ReleaseError('Invalid baseline image identity')
            found = subprocess.run(['docker', 'image', 'inspect', image], capture_output=True)
            if found.returncode:
                archive = self.directory / ('baseline-' + image[7:19] + '.tar')
                with archive.open('wb') as output:
                    result = subprocess.run([SSH, '-o', 'BatchMode=yes', self.config['host'],
                        shlex.join(['docker', 'save', image])], stdout=output, stderr=subprocess.PIPE, timeout=600)
                if result.returncode:
                    raise ReleaseError('Cannot retrieve the deployed baseline image')
                run(['docker', 'load', '-i', str(archive)], timeout=300)
        from release_baseline import sync
        sync(self)

    def activate(self):
        self.gate()
        if self.state.get('pricing_digest') and digest(self.production_pricing()) != self.state['pricing_digest']:
            raise ReleaseError('Production pricing changed after rehearsal; revalidate before cutover')
        target = self.remote_path()
        existing = subprocess.run([SSH, '-o', 'BatchMode=yes', self.config['host'],
            shlex.join(['cat', target + '/receipt.json'])], capture_output=True)
        if existing.returncode == 0:
            receipt = json.loads(existing.stdout)
            if receipt.get('result') == 'passed' and receipt.get('release_id') == self.id:
                return
        # Deterministic server job: reconnect observes the same job, never starts
        # another migration. Explicit recover is used only after a failed result.
        self.ssh(['sh', target + '/rolling-remote.sh', 'release', self.id,
                  self.config['remote_root']], timeout=7200, log=self.directory / 'remote.log')

    def acceptance(self):
        target = self.remote_path()
        receipt = json.loads(self.ssh(['cat', target + '/receipt.json']))
        if receipt.get('result') != 'passed' or receipt.get('release_id') != self.id:
            raise ReleaseError('Remote release has no successful receipt')
        current = json.loads(self.ssh(['cat', self.config['remote_root'] + '/.rolling/state.json']))
        if current.get('release_operation') != self.id or current.get('phase') != 'stable':
            raise ReleaseError('Another operation changed the active production state')
        results = []
        for url in self.config.get('public_checks', []):
            # curl supports the deployed edge path where Python urllib may be blocked.
            body = run(['curl', '--fail', '--silent', '--show-error', '--max-time', '15', url])
            payload = json.loads(body)
            if payload.get('success') is not True:
                raise ReleaseError('Public acceptance failed')
            results.append({'url': url, 'result': 'passed'})
        if not results:
            raise ReleaseError('Configure public acceptance checks before publishing')
        save(self.directory / 'acceptance.json', {'result': 'passed', 'checks': results, 'receipt': receipt})

    def execute(self, prepare_only=False, local_only=False):
        os.environ['RELEASE_DIR'] = str(self.directory)
        self.assert_inputs()
        if self.state['plan']['mode'] == 'validation' and self.state['plan']['checks'] == ['targeted_tests']:
            # Test-only changes do not upload/reload any production component.
            self.persist(candidate='none', candidate_image_id='none')
            self.step('validation', lambda: run([sys.executable, str(ROOT / 'scripts/release-checks.py'),
                'targeted_tests', str(self.directory)], log=self.directory / 'targeted_tests.log', timeout=1800))
            self.persist(phase='complete')
            return
        local_only = local_only or self.state['plan']['mode'] == 'validation'
        if self.state.get('validation_only') and not local_only:
            raise ReleaseError('This is local-only evidence; create a committed release to publish')
        steps = [('fixtures', self.fixtures), ('build', self.build), ('validation', self.validate)]
        if not local_only:
            steps = [('tag', self.tag)] + steps + [('upload', self.upload)]
        for name, operation in steps:
            self.step(name, operation)
        if local_only:
            self.persist(phase='locally_verified')
            self.prune()
            return
        self.persist(phase='prepared')
        if prepare_only:
            return
        self.step('switch', self.activate)
        self.step('acceptance', self.acceptance)
        self.persist(phase='complete')
        self.prune()
        print('发布完成：' + self.id, flush=True)

    def prune(self):
        """Delete this release's rehearsal environments; keep evidence and records.

        Only resources named after this release's validation clone or its own
        drill directories are touched; the development instance, the check
        database and the named build caches are never in scope. Absorbed
        baseline generations stay: a later rollback still needs them. A failure
        here never fails a finished release.
        """
        names = {self.state.get('validation_target', {}).get('prefix', '')}
        for pattern in ('pricing-drill-*', 'gateway-dns-*', 'backup-drill-*', 'release-restore-*'):
            for path in self.directory.glob(pattern):
                # Drill directories append a random suffix to the resource
                # prefix: pricing-drill-<8hex>-<random>.
                if path.is_dir() and '-' in path.name:
                    names.add('-'.join(path.name.split('-')[:-1]))
        names = sorted(name for name in names if name)
        if not names:
            return
        containers = sorted({name for name in docker_list('container') for target in names
                             if name.startswith(target + '-')})
        volumes = sorted({name for name in docker_list('volume') for target in names
                          if name.startswith(target)})
        networks = sorted({name for name in docker_list('network') for target in names
                           if name.startswith(target)})
        try:
            for command, targets in ((['docker', 'rm', '-f'], containers),
                                     (['docker', 'volume', 'rm'], volumes),
                                     (['docker', 'network', 'rm'], networks)):
                if targets:
                    run(command + targets)
            for folder in sorted(self.directory.iterdir()):
                if not folder.is_dir() or not folder.name.startswith(
                        ('pricing-drill-', 'gateway-dns-', 'backup-drill-', 'release-restore-')):
                    continue
                shutil.rmtree(folder)
            self.persist(pruned=True)
            print('演练环境已清理：容器 %d、卷 %d、网络 %d' % (len(containers), len(volumes), len(networks)), flush=True)
        except (ReleaseError, OSError, subprocess.TimeoutExpired) as error:
            print('演练环境清理失败（不影响发布结果）：' + str(error), flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['plan', 'verify', 'prepare', 'run', 'resume', 'status', 'recover', 'cleanup', 'rollback', 'retire-rollback'])
    parser.add_argument('--config', type=Path, default=PRIVATE / 'config.json')
    parser.add_argument('--release')
    parser.add_argument('--baseline', help='Read-only plan against a local commit, without SSH')
    parser.add_argument('--maintenance', action='store_true',
                        help='Force a maintenance release, e.g. when the candidate needs a database migration')
    parser.add_argument('--worker-handoff', action='store_true',
                        help='Replace the worker in place even when it is proven unaffected')
    args = parser.parse_args()
    if args.action == 'plan' and args.baseline:
        print(json.dumps(make_plan(ROOT, args.baseline, force_maintenance=args.maintenance), ensure_ascii=False, indent=2))
        return
    config = read(args.config)
    release = Release(config, args.release)
    if args.action == 'plan':
        baseline, remote = release.baseline()
        print(json.dumps(release.plan(baseline, remote, args.maintenance, args.worker_handoff), ensure_ascii=False, indent=2))
    elif args.action == 'status':
        if args.release:
            print(json.dumps(release.state, ensure_ascii=False, indent=2))
        print(release.ssh(['cat', config['remote_root'] + '/.rolling/state.json']))
    else:
        with release_lock():
            if args.action in ('recover', 'cleanup', 'rollback', 'retire-rollback'):
                if not args.release:
                    raise ReleaseError('An explicit release id selects the already validated remote controller')
                target = release.remote_path()
                command = (['sh', target + '/rolling-remote.sh', 'release-recover', config['remote_root']]
                    if args.action == 'recover' else ['sh', target + '/rolling-remote.sh', args.action, '--root', config['remote_root']])
                if args.action == 'retire-rollback':
                    if release.state.get('phase') != 'complete' or release.state.get('validation_only'):
                        raise ReleaseError('Rollback retirement requires a completed published release')
                    command += ['--expected-revision', release.state['plan']['revision'],
                                '--expected-image', release.state['candidate_image_id']]
                print(release.ssh(command, timeout=7200))
            else:
                if not args.release and not release.create(local_only=args.action == 'verify',
                                                           force_maintenance=args.maintenance,
                                                           force_handoff=args.worker_handoff):
                    return
                release.execute(prepare_only=args.action == 'prepare', local_only=args.action == 'verify')


if __name__ == '__main__':
    try:
        main()
    except (ReleaseError, OSError, ValueError, subprocess.TimeoutExpired) as error:
        print('发布未完成：' + str(error), file=sys.stderr)
        sys.exit(1)
