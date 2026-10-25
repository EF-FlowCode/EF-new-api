"""Release planning and evidence contracts. No Docker, network or mutations here."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import time

SCHEMA = 2
SCENARIOS = {'drain_timeout', 'forced_cut', 'backup_failure', 'migration_failure',
             'new_web_start_failure', 'migration_client_disconnect', 'successful_upgrade'}
BASE_CHECKS = ['backup', 'existing_database_upgrade', 'restart_idempotence', 'api_and_ui']
MAINTENANCE_CHECKS = ['all_model_pricing', 'billing_equivalence', 'maintenance_recovery', 'backup_chain_restore',
                      'task_pricing_readiness']

# Reviewed once for this production baseline, not an allowlist for router/.
OAUTH_COMPATIBILITY = {
    'id': 'oauth-critical-limit-20260921',
    'baseline': '7114a494663d0e24f212005215f496a75fd55f01',
    'path': 'router/api-router.go',
    'before': '2089469344826601eece7edada0130e30860863c22745d84fd3961b65b1421c3',
    'after': 'df217877f2aebd4aeb2fc78a2bdc7c29c8bdefdd2cbc072673a96d8669476b89',
    'contract': 'eflow-rolling-v7',
    'worker_image': 'sha256:965adbd602985967a3c75c5bddc792605a4639541f1528c604871e126e3e4cae',
}

# Read-only invoice queries/export plus frontend recovery, reviewed against this
# exact production revision. Any extra application edit invalidates the proof.
INVOICE_COMPATIBILITY = {
    'id': 'invoice-resource-recovery-20260928',
    'baseline': '3f754c9033438508db889441d9e4e23f6204b662',
    'before': '45333891108352bcf8182ed8486794f423b9ec32a4a65360cf17a35d42c03adb',
    'after': 'd769161b72cdd236d536c9216db42a156fed29818d4ced428955077d6f16cd87',
    'contract': 'eflow-rolling-v7',
    'worker_image': 'sha256:275091340c76587287fc87ce09443d1f48b090f053eaec0ab9e080c04a053881',
}


# Read-only log projection plus a standalone, explicitly invoked migration tool.
# No writer, schema, pricing or worker lifecycle change is part of this bridge.
LOG_COMPATIBILITY = {
    'id': 'log-metadata-bridge-20261001',
    'baseline': 'b71e2770a2ff9bd58b480ff02cf7f5c73606c53e',
    'before': '923c502973795fe055de21e513e63be74917fd4d25388ae35d2f85af42271670',
    'after': 'c3c1d44864dcf4d246a16456ef7508808a5967dce0954b9a2c121d69884095c5',
    'contract': 'eflow-rolling-v7',
    'worker_image': 'sha256:275091340c76587287fc87ce09443d1f48b090f053eaec0ab9e080c04a053881',
}

class ReleaseError(RuntimeError):
    pass


# A snapshot backup is trusted only through a real restore drill of the same
# disk, drill tool, host guard and controller; any edit to these falls back.
SNAPSHOT_DRILL_EVIDENCE = '.releases/backup-snapshot-drill.json'
SNAPSHOT_DRILL_MAX_AGE = 30 * 86400
SNAPSHOT_BOUND_FILES = ('scripts/backup-snapshot-drill.py', 'scripts/backup-zfs-guard.sh', 'scripts/rolling-deploy.py')
SNAPSHOT_TARGET_KEYS = ('dataset', 'fs_uuid', 'mount')


def backup_strategy(root, config, now=None):
    """Return (strategy, reason). Configuration alone never grants zfs_snapshot."""
    wanted = (config or {}).get('backup_strategy', 'stopped_full')
    if wanted == 'stopped_full':
        return 'stopped_full', None
    if wanted != 'zfs_snapshot':
        raise ReleaseError('Unsupported backup_strategy: ' + str(wanted))
    target = (config or {}).get('snapshot_backup') or {}
    path = Path(root) / SNAPSHOT_DRILL_EVIDENCE
    if not path.exists():
        return 'stopped_full', '快照备份缺少恢复演练证据，本次仍用 stopped_full'
    record = json.loads(path.read_text(encoding='utf-8'))
    problems = []
    if record.get('result') != 'passed':
        problems.append('演练未通过')
    if (now if now is not None else time.time()) - float(record.get('finished', 0)) > SNAPSHOT_DRILL_MAX_AGE:
        problems.append('演练已超过 30 天')
    if any(not target.get(key) or (record.get('target') or {}).get(key) != target.get(key) for key in SNAPSHOT_TARGET_KEYS):
        problems.append('演练对象与配置的磁盘不一致')
    for name in SNAPSHOT_BOUND_FILES:
        if (record.get('files') or {}).get(name) != file_hash(Path(root) / name):
            problems.append(name + ' 已在演练后修改')
    if problems:
        return 'stopped_full', '快照恢复演练证据失效（' + '；'.join(problems) + '），本次仍用 stopped_full'
    return 'zfs_snapshot', None


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                     separators=(',', ':')).encode()).hexdigest()


def file_hash(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def git(root, *args):
    result = subprocess.run(['git', '-C', str(root), *args], capture_output=True)
    if result.returncode:
        raise ReleaseError('Git inspection failed')
    return result.stdout.decode('utf-8').strip()


def domain(path):
    """Unknown files are application inputs, never silently ignored.

    Embedded plugin assets stay application inputs even when named tests.
    This inventory is also the sole input of the production build context.
    """
    if path.startswith(('docs/', '.agents/', '.github/ISSUE_TEMPLATE/')) or (
            path.endswith('.md') and path != 'THIRD-PARTY-LICENSES.md'):
        return 'docs'
    if not path.startswith('plugins/') and (re.search(r'(_test\.go|\.(test|spec)\.[^/]+)$', path)
            or '/__tests__/' in path or path.startswith(('test/', 'tests/'))
            or (path.startswith('scripts/') and (path.endswith('-integration.py') or path in (
                'scripts/release-ui.cjs', 'scripts/task-pricing-preflight.cjs', 'scripts/pricing_equivalence.py',
                'scripts/rolling-test-upstream.cjs', 'scripts/billing-test-upstream.cjs',
                'scripts/bluegreen-probe.cjs')))):
        return 'tests'
    if path.startswith(('scripts/', '.github/workflows/')) or path in (
            'examples/release.config.json',
            'menu.ps1', 'Dockerfile.dev', 'Dockerfile.systemone-proxy') or path.startswith('docker-compose'):
        return 'deployment'
    if path in ('.gitignore', '.gitattributes', '.editorconfig', 'LICENSE', 'LICENSE_CN'):
        return 'docs'
    return 'application'


def inventory(root):
    paths = git(root, 'ls-files', '-z', '--cached', '--others', '--exclude-standard').split('\0')
    groups = {key: {} for key in ('application', 'deployment', 'tests', 'docs')}
    for name in sorted(set(filter(None, paths))):
        path = Path(root) / name
        if path.is_symlink():
            raise ReleaseError('Release inputs must not be symlinks: ' + name)
        if path.is_file():
            groups[domain(name)][name] = file_hash(path)
    return groups


def fingerprints(root):
    return {key: digest(files) for key, files in inventory(root).items()}


def classify(paths, contract_changed=False, worker_impact=None, force_maintenance=False):
    """Return (mode, reasons, worker) where worker is 'retain' or 'handoff' for blue-green."""
    changed = set(paths)
    categories = {domain(p) for p in changed}
    if not categories or categories <= {'docs'}:
        return 'docs', ['仅文档，不构建应用'], None
    if categories <= {'docs', 'tests'}:
        return 'validation', ['测试变化，仅更新验证证据'], None
    if 'application' not in categories:
        return 'components', ['部署组件变化，执行网关配置和恢复验证'], None
    if force_maintenance:
        return 'maintenance', ['手动指定维护发布（-Maintenance）'], None
    if contract_changed:
        return 'maintenance', ['部署契约变化，新旧实例不能混跑；完整维护验证'], None
    runtime = sorted(p for p in changed if domain(p) == 'application')
    backend = [p for p in runtime if not p.startswith('web/')]
    if not backend:
        return 'blue_green', ['仅页面资源，与现有后台兼容'], 'retain'
    # Same contract: web switches blue-green and the worker is handed off in
    # place. The worker keeps its image only when that is provably safe, which
    # avoids the background pause; otherwise the handoff keeps both on one release.
    reasons = ['后台变化、契约不变：Web 蓝绿切换，本地克隆证明无需数据库迁移']
    opaque = [p for p in backend if not p.endswith('.go')]
    if opaque:
        return 'blue_green', reasons + ['后台非 Go 输入变化（' + '、'.join(opaque[:6]) + '），worker 原地交接到新版本'], 'handoff'
    if not worker_impact:
        return 'blue_green', reasons + ['未做 worker 影响分析，worker 原地交接到新版本'], 'handoff'
    if worker_impact.get('error'):
        return 'blue_green', reasons + ['worker 影响分析失败（' + worker_impact['error'] + '），worker 原地交接到新版本'], 'handoff'
    affected = worker_impact.get('affected') or []
    if affected:
        names = lambda items: '、'.join(item['key'].rsplit('/', 1)[-1] for item in items[:8])
        shared = [item for item in affected if item.get('kind') == 'shared_data']
        code = [item for item in affected if item.get('kind') != 'shared_data']
        if code:
            reasons.append('worker 可执行或使用的声明有变化：' + names(code))
        if shared:
            reasons.append('改动可能改变 worker 读取的共享数据：' + names(shared))
        return 'blue_green', reasons + ['worker 原地交接到新版本'], 'handoff'
    return 'blue_green', reasons + ['静态分析证明 worker 不受影响（%d 处声明变化），worker 保留原镜像'
                                    % worker_impact.get('changed', 0)], 'retain'


def audited_compatibility(root, baseline, paths):
    runtime = {p for p in paths if domain(p) == 'application'}
    for declaration in (INVOICE_COMPATIBILITY, LOG_COMPATIBILITY):
        if baseline == declaration['baseline']:
            before, after = {}, {}
            for name in sorted(runtime):
                path = Path(root) / name
                if not path.is_file() or path.is_symlink():
                    return None
                old = subprocess.run(['git', '-C', str(root), 'show', baseline + ':' + name], capture_output=True)
                before[name] = hashlib.sha256(old.stdout.replace(b'\r\n', b'\n')).hexdigest() if old.returncode == 0 else None
                after[name] = hashlib.sha256(path.read_bytes().replace(b'\r\n', b'\n')).hexdigest()
            if digest(before) == declaration['before'] and digest(after) == declaration['after']:
                return dict(declaration)
            return None
    declaration = OAUTH_COMPATIBILITY
    if baseline != declaration['baseline'] or runtime != {declaration['path']}:
        return None
    before = subprocess.run(['git', '-C', str(root), 'show', baseline + ':' + declaration['path']],
                            capture_output=True, check=True).stdout
    path = Path(root) / declaration['path']
    if not path.is_file() or path.is_symlink():
        return None
    # Git stores LF; Windows checkouts may use CRLF. Nothing else is normalized.
    hashes = [hashlib.sha256(data.replace(b'\r\n', b'\n')).hexdigest()
              for data in (before, path.read_bytes())]
    if hashes != [declaration['before'], declaration['after']]:
        return None
    return dict(declaration)


def validate_worker_impact_target(plan, state, worker_label):
    """A Go-backend blue-green plan is valid only against the analyzed worker."""
    impact = plan.get('worker_impact')
    if plan.get('mode') != 'blue_green' or not impact:
        return
    if (impact.get('error') or impact.get('affected') or worker_label != impact.get('worker_fingerprint')
            or state.get('contract') != plan.get('target_contract')
            or state.get('release_revision') != plan.get('baseline')):
        raise ReleaseError('Running worker differs from the analyzed worker source; plan again')


def validate_compatibility_target(plan, state, worker_image):
    declaration = plan.get('compatibility')
    if declaration and (declaration not in (OAUTH_COMPATIBILITY, INVOICE_COMPATIBILITY, LOG_COMPATIBILITY)
            or plan['mode'] != 'blue_green' or plan['baseline'] != declaration['baseline']
            or state.get('contract') != declaration['contract']
            or state.get('worker_image') != declaration['worker_image']
            or worker_image != declaration['worker_image']):
        raise ReleaseError('Audited compatibility does not match the deployed worker/contract')


def worker_impact_summary(root, worker, analyze, paths):
    """Run the injected analyzer only for Go-only backend changes.

    `worker` names the source revision and image fingerprint of the running
    worker; `analyze(root, revision)` returns the release-impact report. This
    module never runs Docker itself.
    """
    backend = [p for p in paths if domain(p) == 'application' and not p.startswith('web/')]
    if not worker or not analyze or not backend or any(not p.endswith('.go') for p in backend):
        return None
    summary = {'worker_revision': worker['revision'], 'worker_fingerprint': worker['fingerprint'],
               'tool': worker.get('tool')}
    try:
        report = analyze(root, worker['revision'])
    except ReleaseError as error:
        return summary | {'error': str(error)}
    return summary | {'analysis_version': report.get('analysis_version'), 'changed': len(report['changed']),
                      'changed_keys': [c['key'] for c in report['changed']][:50],
                      'affected': report['worker_affected']}


def contract_at(root, revision):
    try:
        text = git(root, 'show', revision + ':internal/deployment/lifecycle.go')
    except ReleaseError:
        return None
    found = re.findall(r'^const Contract = "([A-Za-z0-9._-]+)"', text, re.MULTILINE)
    return found[0] if len(found) == 1 else None


def make_plan(root, baseline, config=None, now=None, worker=None, analyze=None, live_contract=None,
              force_maintenance=False, force_handoff=False):
    revision = git(root, 'rev-parse', 'HEAD')
    baseline = git(root, 'rev-parse', baseline + '^{commit}')
    paths = set(filter(None, git(root, 'diff', '--name-only', '-z', baseline).split('\0')))
    paths.update(filter(None, git(root, 'ls-files', '--others', '--exclude-standard', '-z').split('\0')))
    target_contract = application_contract(root)
    # Unknown baseline contract counts as changed: only a proven-equal contract may mix versions.
    contract_changed = (contract_at(root, baseline) != target_contract
                        or (live_contract is not None and live_contract != target_contract))
    impact = None if contract_changed else worker_impact_summary(root, worker, analyze, paths)
    mode, reasons, worker_mode = classify(paths, contract_changed, impact, force_maintenance)
    compatibility = audited_compatibility(root, baseline, paths)
    if compatibility:
        mode, reasons, worker_mode = 'blue_green', ['精确绑定已审应用差异、线上基线、契约与现有 worker：' + compatibility['id']], 'retain'
    if force_handoff and mode == 'blue_green' and not compatibility and worker_mode == 'retain':
        worker_mode = 'handoff'
        reasons = reasons + ['手动指定 worker 原地交接（-WorkerHandoff）']
    backend_changed = mode == 'blue_green' and not compatibility and any(
        domain(p) == 'application' and not p.startswith('web/') for p in paths)
    checks = [] if mode == 'docs' else ['targeted_tests']
    if mode not in ('docs', 'validation'):
        checks += BASE_CHECKS
    if mode in ('components', 'maintenance'):
        checks += ['gateway_recovery', 'maintenance_recovery']
    if mode == 'maintenance':
        checks += MAINTENANCE_CHECKS
    if mode == 'validation':
        if any(p in ('scripts/rolling-gateway-integration.py', 'scripts/release-supervisor-integration.py') for p in paths):
            checks += ['gateway_recovery']
        if any(p in ('scripts/maintenance-integration.py', 'scripts/billing-integration.py',
                     'scripts/billing-test-upstream.cjs', 'scripts/rolling-test-upstream.cjs', 'scripts/pricing_equivalence.py') for p in paths):
            checks += ['maintenance_recovery', 'all_model_pricing', 'billing_equivalence']
        if 'scripts/release-ui.cjs' in paths:
            checks += BASE_CHECKS
    if any(p.startswith('scripts/backup') for p in paths) and mode != 'docs':
        checks += ['backup_chain_restore']
    if mode == 'blue_green' and any(domain(p) == 'deployment' for p in paths):
        checks += ['gateway_recovery']
    if compatibility in (INVOICE_COMPATIBILITY, LOG_COMPATIBILITY):
        checks += ['blue_green_compatibility']
    if backend_changed:
        # Billing runs in web, so any backend change re-proves pricing and
        # charges; the coexistence rehearsal covers web switch, worker handoff,
        # original cookies, one SSE charge and rollback for real.
        checks += ['blue_green_compatibility', 'maintenance_recovery', 'all_model_pricing', 'billing_equivalence']
    if mode == 'validation' and set(checks) != {'targeted_tests'}:
        checks += BASE_CHECKS
    strategy = 'stopped_full'
    if mode == 'maintenance':
        strategy, note = backup_strategy(root, config, now)
        if note:
            reasons = reasons + [note]
    plan = {'schema': SCHEMA, 'revision': revision, 'baseline': baseline, 'mode': mode,
            'validation_contract': 'baseline-clone-v1',
            'target_contract': target_contract,
            'changes': sorted(paths), 'reasons': reasons, 'fingerprints': fingerprints(root),
            'checks': sorted(set(checks)), 'build': mode in ('blue_green', 'maintenance'),
            # Maintenance: grace before the gateway cut, then settlement budget.
            'drain_grace_seconds': 10, 'drain_seconds': 120, 'backup_strategy': strategy,
            'worker_handoff_verified': False}
    if mode == 'blue_green':
        plan['worker'] = worker_mode
        plan['backend_changed'] = backend_changed
    if compatibility:
        plan['compatibility'] = compatibility
    if impact and mode == 'blue_green' and worker_mode == 'retain' and not compatibility:
        plan['worker_impact'] = impact
    # Documentation content does not invalidate the execution plan or evidence.
    plan['id'] = digest({k: v for k, v in plan.items() if k not in ('changes', 'fingerprints')} |
                        {'fingerprints': {k: v for k, v in plan['fingerprints'].items() if k != 'docs'}})
    return plan


def application_contract(root):
    """Bind the planned contract to both the application and deployment source."""
    root = Path(root)
    app = re.findall(r'^const Contract = "([A-Za-z0-9._-]+)"',
                     (root / 'internal/deployment/lifecycle.go').read_text(encoding='utf-8'), re.MULTILINE)
    controller = re.findall(r'^CONTRACT = "([A-Za-z0-9._-]+)"',
                     (root / 'scripts/rolling-deploy.py').read_text(encoding='utf-8'), re.MULTILINE)
    if len(app) != 1 or app != controller:
        raise ReleaseError('Application and deployment contract must match exactly')
    return app[0]


def check_binding(name, plan):
    # Database upgrade proofs are independent of unrelated test edits. The
    # controller remains bound because it creates and validates those proofs.
    keys = ('application', 'deployment') if name in BASE_CHECKS else ('application', 'deployment', 'tests')
    if name == 'gateway_recovery':
        keys = ('deployment', 'tests')
    return digest({key: plan['fingerprints'][key] for key in keys})


def contained(root, relative):
    root = Path(root).resolve()
    path = (root / relative).resolve()
    if not path.is_relative_to(root) or path == root:
        raise ReleaseError('Evidence path escapes release directory')
    return path


def validate_evidence(plan, record, directory, candidate, live_image):
    if record.get('schema') != SCHEMA or record.get('result') != 'passed':
        raise ReleaseError('Only schema 2 passed evidence is accepted; old records must be revalidated')
    if record.get('plan_id') != plan['id']:
        raise ReleaseError('Evidence does not belong to the current release plan')
    if record.get('candidate_image_id') != candidate or live_image != candidate:
        raise ReleaseError('The existing local instance must run the same validated image')
    if (record.get('validation_contract') != 'baseline-clone-v1'
            or not re.fullmatch(r'newapi-upgrade-[a-f0-9]{12}-web', record.get('container', ''))):
        raise ReleaseError('Only a fresh production-baseline clone can satisfy the upgrade gate')
    baseline_path = contained(directory, 'baseline.json')
    if file_hash(baseline_path) != record.get('baseline_manifest_sha256'):
        raise ReleaseError('Baseline manifest changed')
    baseline = json.loads(baseline_path.read_text(encoding='utf-8'))
    if (baseline.get('result') != 'passed' or baseline.get('id') != record.get('baseline_generation')
            or baseline.get('scope') != 'production_schema_pricing_sanitized_samples'
            or baseline.get('production', {}).get('revision') != plan.get('baseline')):
        raise ReleaseError('Baseline provenance does not match the release plan')
    entries = record.get('checks', [])
    for name in plan['checks']:
        matches = [x for x in entries if x.get('name') == name]
        if len(matches) != 1:
            raise ReleaseError('Missing or duplicate evidence: ' + name)
        item = matches[0]
        path = contained(directory, item['file'])
        if item.get('sha256') != file_hash(path):
            raise ReleaseError('Evidence has changed: ' + name)
        proof = json.loads(path.read_text(encoding='utf-8-sig'))
        if proof.get('result') != 'passed' or proof.get('candidate_image_id') != candidate:
            raise ReleaseError('Unverified candidate/check: ' + name)
        if proof.get('input_fingerprint') != check_binding(name, plan):
            raise ReleaseError('Evidence inputs changed: ' + name)
        if (proof.get('baseline_generation') != record['baseline_generation']
                or proof.get('baseline_manifest_sha256') != record['baseline_manifest_sha256']):
            raise ReleaseError('Check belongs to a different baseline generation: ' + name)
        if name == 'backup':
            archive = proof.get('container_logs') or {}
            files = proof.get('files', {})
            if (not archive.get('container_id') or archive.get('image_id') != proof.get('previous_image_id')
                    or len(archive.get('files', {})) != 4
                    or not {'database.sql', 'data.tar.gz'} <= set(files)
                    or any(files.get(n) != h for n, h in archive.get('files', {}).items())):
                raise ReleaseError('Missing database/data/container audit log backup evidence')
            if any(file_hash(contained(directory, n)) != h for n, h in files.items()):
                raise ReleaseError('Local backup files have changed')
        if name == 'existing_database_upgrade' and (proof.get('container') != record['container']
                or proof.get('clone_owner') != record.get('release_id')
                or proof.get('cloned_from_baseline') is not True
                or proof.get('clone_seed_sha256') != baseline.get('files', {}).get('database.sql')
                or proof.get('previous_image_id') != baseline['production']['web_image']
                or not all(proof.get(k) for k in ('previous_image_id', 'previous_version', 'candidate_version'))):
            raise ReleaseError('Missing real baseline clone upgrade identity')
        if name == 'maintenance_recovery':
            passed = {s['name'] for s in proof.get('scenarios', []) if s.get('result') == 'passed'}
            if not SCENARIOS <= passed or not proof.get('isolated_resources_cleaned'):
                raise ReleaseError('All seven real maintenance recovery scenarios are required')
        if name == 'task_pricing_readiness' and not task_pricing_proof_ok(proof):
            raise ReleaseError('A preview alone cannot prove completed task pricing migration')
    return True


def task_pricing_proof_ok(proof):
    common = (proof.get('task_priority_version') == 1
        and proof.get('preview', {}).get('already_applied') is True
        and proof.get('preview', {}).get('blocked') is False
        and proof.get('preview_options_unchanged') is True
        and proof.get('restart_idempotent') is True
        and proof.get('actor_restored') is True
        and bool(proof.get('after_http', {}).get('cases')))
    if proof.get('migrated_baseline') is True:
        # Baseline already migrated: independent HTTP billing against unchanged
        # migration-target prices, plus a no-op repeat apply.
        return (common and proof.get('provenance_matches_current') is True
            and proof.get('repeat_apply_noop') is True)
    return (common and proof.get('migration_verified') is True
        and proof.get('http_equivalent') is True
        and proof.get('before_http', {}).get('cases') == proof.get('after_http', {}).get('cases'))
