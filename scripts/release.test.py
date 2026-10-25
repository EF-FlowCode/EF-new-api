"""Local unit tests of release safety contracts; no Docker/SSH/build required."""
import importlib.util
import hashlib
import json
import os
from pathlib import Path
import tempfile
import subprocess
import unittest
from types import SimpleNamespace
from unittest.mock import Mock, patch

import release
import release_core as core
import release_remote as remote
import release_local as local
import release_baseline as baseline
import release_ui as ui
from pricing_equivalence import compare as compare_pricing

spec = importlib.util.spec_from_file_location('release_checks', Path(__file__).with_name('release-checks.py'))
checks = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checks)


def attach_baseline(directory, plan, proof, record, old='old'):
    manifest = {'id': 'generation', 'result': 'passed', 'scope': baseline.SCOPE,
                'production': {'revision': 'baseline-revision', 'web_image': old},
                'files': {'database.sql': 'seed-sha256'}}
    release.save(Path(directory) / 'baseline.json', manifest)
    checksum = core.file_hash(Path(directory) / 'baseline.json')
    plan['baseline'] = 'baseline-revision'
    record.update(validation_contract='baseline-clone-v1', release_id='release-test',
                  container='newapi-upgrade-0123456789ab-web', baseline_generation='generation',
                  baseline_manifest_sha256=checksum)
    proof.update(container=record['container'], clone_owner='release-test', cloned_from_baseline=True,
                 clone_seed_sha256='seed-sha256', baseline_generation='generation', baseline_manifest_sha256=checksum)
    entry = record['checks'][0]
    release.save(Path(directory) / entry['file'], proof)
    entry['sha256'] = core.file_hash(Path(directory) / entry['file'])


class ReleaseTests(unittest.TestCase):
    def test_task_pricing_proof_accepts_migrated_baseline_only_with_its_own_evidence(self):
        cases = [{'model': 'm', 'expected': 5}]
        base = {'task_priority_version': 1, 'preview': {'already_applied': True, 'blocked': False},
                'preview_options_unchanged': True, 'restart_idempotent': True, 'actor_restored': True,
                'after_http': {'cases': cases}}
        unmigrated = dict(base, migration_verified=True, http_equivalent=True, before_http={'cases': cases})
        self.assertTrue(core.task_pricing_proof_ok(unmigrated))
        self.assertFalse(core.task_pricing_proof_ok(dict(unmigrated, before_http={'cases': []})))
        migrated = dict(base, migrated_baseline=True, provenance_matches_current=True, repeat_apply_noop=True)
        self.assertTrue(core.task_pricing_proof_ok(migrated))
        # Prices edited after migration, no HTTP matrix or no repeat-apply check: reject.
        for key, value in (('provenance_matches_current', False), ('repeat_apply_noop', None),
                           ('after_http', {'cases': []}), ('restart_idempotent', None)):
            self.assertFalse(core.task_pricing_proof_ok(dict(migrated, **{key: value})), key)
        # A migrated-baseline proof cannot borrow the unmigrated path's flags alone.
        self.assertFalse(core.task_pricing_proof_ok(dict(base, migrated_baseline=True)))

    def test_log_bridge_declaration_rejects_extra_or_modified_runtime(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'model').mkdir()
            (root / 'model/log_other.go').write_bytes(b'new bridge\n')
            (root / 'model/extra.go').write_bytes(b'extra\n')
            declaration = dict(core.LOG_COMPATIBILITY,
                before=core.digest({'model/log_other.go': None}),
                after=core.digest({'model/log_other.go': hashlib.sha256(b'new bridge\n').hexdigest()}))
            with patch.object(core, 'LOG_COMPATIBILITY', declaration), patch.object(core.subprocess, 'run',
                    return_value=subprocess.CompletedProcess([], 1, stdout=b'')):
                self.assertEqual(core.audited_compatibility(root, declaration['baseline'], ['model/log_other.go']), declaration)
                self.assertIsNone(core.audited_compatibility(root, declaration['baseline'], ['model/log_other.go', 'model/extra.go']))
                (root / 'model/log_other.go').write_bytes(b'changed bridge\n')
                self.assertIsNone(core.audited_compatibility(root, declaration['baseline'], ['model/log_other.go']))
            plan = {'compatibility': core.LOG_COMPATIBILITY, 'mode': 'blue_green', 'baseline': core.LOG_COMPATIBILITY['baseline']}
            state = {key: core.LOG_COMPATIBILITY[key] for key in ('worker_image', 'contract')}
            core.validate_compatibility_target(plan, state, state['worker_image'])
            with self.assertRaises(core.ReleaseError):
                core.validate_compatibility_target(plan, state, 'changed worker')

    def test_invoice_declaration_requires_exact_application_delta(self):
        name, old, new = 'model/ticket.go', b'before\n', b'after\n'
        declaration = dict(core.INVOICE_COMPATIBILITY,
            before=core.digest({name: hashlib.sha256(old).hexdigest()}),
            after=core.digest({name: hashlib.sha256(new).hexdigest()}))
        with tempfile.TemporaryDirectory() as folder, patch.object(core, 'INVOICE_COMPATIBILITY', declaration):
            path = Path(folder) / name
            path.parent.mkdir()
            path.write_bytes(new)
            def check(paths, revision=declaration['baseline']):
                with patch.object(core.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, stdout=old)):
                    return core.audited_compatibility(folder, revision, paths)
            self.assertEqual(check([name, 'docs/example.md']), declaration)
            self.assertIsNone(check([name], 'other'))
            self.assertIsNone(check([name, 'service/quota.go']))
            path.write_bytes(new + b'extra')
            self.assertIsNone(check([name]))
            path.unlink()
            self.assertIsNone(check([name]))

    def test_invoice_target_keeps_worker_and_contract_bound(self):
        declaration = core.INVOICE_COMPATIBILITY
        plan = {'compatibility': declaration, 'mode': 'blue_green', 'baseline': declaration['baseline']}
        state = {k: declaration[k] for k in ('worker_image', 'contract')}
        core.validate_compatibility_target(plan, state, state['worker_image'])
        for changed in (dict(state, contract='other'), dict(state, worker_image='other')):
            with self.assertRaises(core.ReleaseError):
                core.validate_compatibility_target(plan, changed, state['worker_image'])
        with self.assertRaises(core.ReleaseError):
            core.validate_compatibility_target(plan, state, 'other')

    def test_oauth_declaration_accepts_only_exact_reviewed_delta(self):
        declaration = core.OAUTH_COMPATIBILITY
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / declaration['path']
            path.parent.mkdir()
            fixture = os.environ.get('RELEASE_OAUTH_BASELINE_FILE')
            before = (Path(fixture).read_bytes() if fixture else subprocess.check_output(
                ['git', 'show', declaration['baseline'] + ':' + declaration['path']], cwd=release.ROOT))
            after = before.replace(b'apiRouter.GET("/oauth/state", middleware.CriticalRateLimit(),',
                b'// OAuth initialization and callbacks do not share the critical IP bucket\n\t\t// with password login: proxy users must not block each other\'s OAuth flow.\n\t\tapiRouter.GET("/oauth/state",')
            after = after.replace(b'apiRouter.GET("/oauth/:provider", middleware.CriticalRateLimit(),', b'apiRouter.GET("/oauth/:provider",')
            path.write_bytes(after)
            def check(baseline, paths):
                with patch.object(core.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, stdout=before)):
                    return core.audited_compatibility(directory, baseline, paths)
            self.assertEqual(check(declaration['baseline'], [declaration['path']]), declaration)
            self.assertIsNone(check('other', [declaration['path']]))
            self.assertIsNone(check(declaration['baseline'], [declaration['path'], 'service/quota.go']))
            self.assertIsNone(check(declaration['baseline'], [declaration['path'], 'Dockerfile']))
            path.write_bytes(after + b'// extra change\n')
            self.assertIsNone(check(declaration['baseline'], [declaration['path']]))
            path.unlink()
            self.assertIsNone(check(declaration['baseline'], [declaration['path']]))

    def test_oauth_target_rejects_different_worker_or_contract(self):
        declaration = core.OAUTH_COMPATIBILITY
        plan = {'compatibility': declaration, 'mode': 'blue_green', 'baseline': declaration['baseline']}
        state = {k: declaration[k] for k in ('worker_image', 'contract')}
        core.validate_compatibility_target(plan, state, state['worker_image'])
        for changed, image in [(dict(state, contract='other'), state['worker_image']),
                               (dict(state, worker_image='other'), state['worker_image']), (state, 'other')]:
            with self.assertRaises(core.ReleaseError):
                core.validate_compatibility_target(plan, changed, image)

    def test_blue_green_rejects_worker_restart(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = Mock()
            deployment.inspect.return_value = {'Id': 'id', 'Image': 'old', 'State': {'Running': True, 'StartedAt': 'later'}}
            release.save(Path(directory) / 'prepared.json', {'worker_identity': {'id': 'id', 'image': 'old', 'started': 'before'}})
            with self.assertRaises(core.ReleaseError):
                remote.require_unchanged_worker(deployment, Path(directory), {'plan': {'mode': 'blue_green'}})

    def test_backend_changes_hand_off_worker_unless_contract_changes(self):
        for path in ('service/quota.go', 'controller/relay.go', 'model/main.go', 'unknown.cfg', 'Dockerfile'):
            mode, _, worker = core.classify([path])
            self.assertEqual((mode, worker), ('blue_green', 'handoff'), path)
            self.assertEqual(core.classify([path], True)[0], 'maintenance')
            self.assertEqual(core.classify([path], False, None, True)[0], 'maintenance')

    def test_known_scopes(self):
        for files, mode in [(['docs/example.md'], 'docs'), (['controller/relay_test.go'], 'validation'),
                            (['scripts/rolling-deploy.py'], 'components'), (['web/src/main.tsx'], 'blue_green')]:
            self.assertEqual(core.classify(files)[0], mode)
        self.assertEqual(core.domain('plugins/tasks/provider/test.js'), 'application')
        self.assertEqual(core.domain('THIRD-PARTY-LICENSES.md'), 'application')

    def test_maintenance_always_requires_backup_restore_even_without_backup_edits(self):
        with patch.object(core, 'git', side_effect=['head', 'baseline', 'service/quota.go', '']), \
                patch.object(core, 'contract_at', return_value=None), \
                patch.object(core, 'fingerprints', return_value={'application': 'a', 'deployment': 'd', 'tests': 't'}):
            plan = core.make_plan('.', 'baseline')
        self.assertEqual(plan['mode'], 'maintenance')
        self.assertTrue(plan['build'])
        self.assertTrue({'all_model_pricing', 'billing_equivalence', 'api_and_ui',
                         'maintenance_recovery', 'backup_chain_restore'} <= set(plan['checks']))

    def test_worker_keeps_image_only_with_proof(self):
        unaffected = {'changed': 3, 'affected': []}
        self.assertEqual(core.classify(['model/usage_refund.go', 'web/src/a.tsx'], False, unaffected)[::2],
                         ('blue_green', 'retain'))
        for paths, impact in [
            (['model/usage_refund.go'], None),
            (['model/usage_refund.go'], {'error': 'load failed'}),
            (['model/usage_refund.go'], {'changed': 1, 'affected': [{'key': 'eflow.local/new-api/model.SyncOptions'}]}),
            (['controller/a.go'], {'changed': 1, 'affected': [{'key': 'eflow.local/new-api/controller.Save',
                                                               'kind': 'shared_data'}]}),
            (['model/usage_refund.go', 'go.mod'], unaffected),
            (['i18n/locales/zh.yaml'], unaffected),
        ]:
            self.assertEqual(core.classify(paths, False, impact)[::2], ('blue_green', 'handoff'), paths)
        self.assertEqual(core.classify(['model/usage_refund.go'], True, unaffected)[0], 'maintenance')
        self.assertEqual(core.classify(['web/src/a.tsx'], True)[0], 'maintenance')
        self.assertEqual(core.classify(['web/src/a.tsx'])[::2], ('blue_green', 'retain'))

    def make_plan(self, changed, baseline_contract='c1', **kwargs):
        with patch.object(core, 'git', side_effect=['head', 'baseline', changed, '']), \
                patch.object(core, 'application_contract', return_value='c1'), \
                patch.object(core, 'contract_at', return_value=baseline_contract), \
                patch.object(core, 'fingerprints', return_value={'application': 'a', 'deployment': 'd', 'tests': 't', 'docs': 'x'}):
            return core.make_plan('.', 'baseline', **kwargs)

    def test_plan_binds_worker_and_requires_billing_and_coexistence_proofs(self):
        worker = {'revision': 'base', 'fingerprint': 'label', 'contract': 'c1', 'tool': 'tool'}
        report = {'analysis_version': 2, 'changed': [{'key': 'm.f'}], 'worker_affected': []}
        calls = []

        def analyze(root, revision):
            calls.append(revision)
            return report
        plan = self.make_plan('model/usage_refund.go', worker=worker, analyze=analyze, live_contract='c1')
        self.assertEqual((plan['mode'], plan['worker'], plan['backend_changed'], calls),
                         ('blue_green', 'retain', True, ['base']))
        self.assertEqual(plan['worker_impact']['worker_fingerprint'], 'label')
        self.assertTrue({'blue_green_compatibility', 'all_model_pricing', 'billing_equivalence',
                         'maintenance_recovery'} <= set(plan['checks']))
        plan = self.make_plan('go.mod', worker=worker, analyze=analyze, live_contract='c1')
        self.assertEqual((plan['mode'], plan['worker'], calls), ('blue_green', 'handoff', ['base']))
        self.assertNotIn('worker_impact', plan)
        report['worker_affected'] = [{'key': 'm.SyncOptions'}]
        self.assertEqual(self.make_plan('model/option.go', worker=worker, analyze=analyze)['worker'], 'handoff')
        web = self.make_plan('web/src/a.tsx')
        self.assertEqual((web['worker'], web['backend_changed']), ('retain', False))
        self.assertNotIn('billing_equivalence', web['checks'])
        for kwargs in ({'baseline_contract': 'c0'}, {'baseline_contract': None}, {'live_contract': 'c0'},
                       {'force_maintenance': True}):
            self.assertEqual(self.make_plan('model/option.go', **kwargs)['mode'], 'maintenance', kwargs)
        forced = self.make_plan('web/src/a.tsx', force_handoff=True)
        self.assertEqual((forced['mode'], forced['worker']), ('blue_green', 'handoff'))

        state = {'contract': 'c1', 'release_revision': 'baseline'}
        target = {'mode': 'blue_green', 'baseline': 'baseline', 'target_contract': 'c1',
                  'worker_impact': {'worker_fingerprint': 'label', 'affected': []}}
        core.validate_worker_impact_target(target, state, 'label')
        for changed_state, label in [(state, 'other'), (state | {'contract': 'c2'}, 'label'),
                                     (state | {'release_revision': 'other'}, 'label')]:
            with self.assertRaises(core.ReleaseError):
                core.validate_worker_impact_target(target, changed_state, label)

    def test_analysis_failure_hands_off_the_worker(self):
        def analyze(root, revision):
            raise core.ReleaseError('docker unavailable')
        plan = self.make_plan('model/usage_refund.go', worker={'revision': 'b', 'fingerprint': 'l', 'contract': 'c1'},
                              analyze=analyze)
        self.assertEqual((plan['mode'], plan['worker']), ('blue_green', 'handoff'))
        self.assertIn('docker unavailable', ' '.join(plan['reasons']))

    def test_handoff_release_checks_new_worker_and_rolls_web_back_on_failure(self):
        deployment = Mock()
        deployment.state = {'phase': 'stable', 'active': 'green', 'worker_image': 'old'}
        manifest = {'plan': {'mode': 'blue_green', 'worker': 'handoff', 'revision': 'new', 'baseline': 'base',
                             'fingerprints': {'application': 'fp-new'}},
                    'candidate_image_id': 'new', 'baseline_state_digest': 'd', 'release_id': 'release-x'}
        with tempfile.TemporaryDirectory() as directory:
            release.save(Path(directory) / 'prepared.json', {'worker_identity': {'id': 'w1', 'image': 'old', 'started': 't',
                                                                                 'fingerprint': 'f'}})
            deployment.inspect.return_value = {'Id': 'w1', 'Image': 'old', 'State': {'Running': True, 'StartedAt': 't'},
                                               'Config': {'Labels': {remote.WORKER_FINGERPRINT_LABEL: 'f'}}}
            with self.assertRaises(core.ReleaseError):
                remote.finish(deployment, Path(directory), manifest)
        self.assertEqual(remote.worker_source_fields(deployment, manifest),
                         {'worker_revision': 'new', 'worker_fingerprint': 'fp-new'})
        # Activation: a failed handoff rolls web back to the previous release.
        deployment = Mock()
        deployment.state = {'phase': 'stable'}
        deployment.handoff_worker.side_effect = remote.rolling.DeployError('new worker failed readiness')
        with patch.object(remote, 'require_unchanged_worker'), patch.object(remote, 'convert_state'), \
                patch.object(remote, 'finish') as finish, patch.object(remote, 'digest', return_value='d'), \
                patch.object(remote, 'file_hash', return_value='m'), tempfile.TemporaryDirectory() as directory:
            release.save(Path(directory) / 'prepared.json', {'manifest_sha256': 'm'})
            with self.assertRaises(core.ReleaseError):
                remote.activate(deployment, Path(directory), manifest)
        deployment.deploy.assert_called_once()
        deployment.rollback.assert_called_once()
        finish.assert_not_called()

    def test_finish_records_worker_source_for_next_analysis(self):
        deployment = Mock()
        deployment.state = {'release_revision': 'old', 'release_fingerprints': {'application': 'fp-old'}}
        maintenance = {'plan': {'mode': 'maintenance', 'revision': 'new', 'fingerprints': {'application': 'fp-new'}}}
        self.assertEqual(remote.worker_source_fields(deployment, maintenance),
                         {'worker_revision': 'new', 'worker_fingerprint': 'fp-new'})
        blue_green = {'plan': {'mode': 'blue_green', 'revision': 'new', 'fingerprints': {'application': 'fp-new'}}}
        deployment.inspect.return_value = {'Id': 'w', 'Image': 'img', 'State': {'Running': True, 'StartedAt': 't'},
                                           'Config': {'Labels': {remote.WORKER_FINGERPRINT_LABEL: 'fp-old'}}}
        self.assertEqual(remote.worker_source_fields(deployment, blue_green),
                         {'worker_revision': 'old', 'worker_fingerprint': 'fp-old'})
        deployment.inspect.return_value['Config']['Labels'][remote.WORKER_FINGERPRINT_LABEL] = 'unknown'
        self.assertEqual(remote.worker_source_fields(deployment, blue_green), {})
        deployment.state['worker_revision'] = 'kept'
        self.assertEqual(remote.worker_source_fields(deployment, blue_green), {})

    def test_release_tests_delegate_scopes_to_docker_entry_only(self):
        for changes, scopes in [(['docs/example.md'], []), (['web/src/main.tsx'], ['--web']),
                                (['relaykit/go.mod'], ['--go']),
                                (['model/main.go', 'web/src/main.tsx'], ['--web', '--go'])]:
            with self.subTest(changes=changes), patch.dict(os.environ, NEW_API_BASH='/bin/bash'), \
                    patch.object(checks.Path, 'is_file', return_value=True), \
                    patch.object(checks.subprocess, 'run', return_value=Mock(returncode=0)) as invoke:
                checks.targeted_tests(changes)
                invoke.assert_called_once_with(['/bin/bash', 'scripts/docker-check.sh', 'release-tests', *scopes],
                                               cwd=release.ROOT, timeout=1800)

    def test_docker_release_test_failure_propagates(self):
        with patch.dict(os.environ, NEW_API_BASH='/bin/bash'), \
                patch.object(checks.Path, 'is_file', return_value=True), \
                patch.object(checks.subprocess, 'run', return_value=Mock(returncode=1)):
            with self.assertRaises(core.ReleaseError):
                checks.targeted_tests(['model/main.go'])

    def test_missing_bash_does_not_fall_back_to_host_tests(self):
        with patch.dict(os.environ, NEW_API_BASH='/missing/bash'), \
                patch.object(checks.Path, 'is_file', return_value=False), \
                patch.object(checks.subprocess, 'run') as invoke:
            with self.assertRaises(core.ReleaseError):
                checks.targeted_tests(['model/main.go'])
            invoke.assert_not_called()

    def test_ui_runs_through_docker_and_propagates_failure(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder)
            release.save(directory / 'state.json', {'candidate_image_id': 'candidate'})
            for code in (0, 1):
                with patch.object(checks, 'docker_check_command', return_value=['bash', 'scripts/docker-check.sh', 'release-ui']), \
                        patch.object(checks.subprocess, 'run', return_value=Mock(returncode=code)) as invoke:
                    if code:
                        with self.assertRaises(core.ReleaseError):
                            checks.main('api_and_ui', directory)
                    else:
                        checks.main('api_and_ui', directory)
                    invoke.assert_called_once_with(['bash', 'scripts/docker-check.sh', 'release-ui', str(directory)],
                                                   cwd=release.ROOT, timeout=300)

    def test_path_traversal_and_symlink_evidence_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(core.ReleaseError):
                core.contained(directory, '../outside.json')

    def test_schema1_cannot_silently_grant_permission(self):
        with self.assertRaises(core.ReleaseError):
            core.validate_evidence({}, {'schema': 1, 'result': 'passed'}, '.', 'a', 'a')

    def test_evidence_checks_content_candidate_and_real_upgrade(self):
        with tempfile.TemporaryDirectory() as directory:
            plan = {'id': 'id', 'checks': ['existing_database_upgrade'], 'fingerprints': {'application': 'a', 'deployment': 'b', 'tests': 'c', 'docs': 'd'}}
            proof = {'result': 'passed', 'candidate_image_id': 'new', 'fingerprints': {'application': 'a', 'deployment': 'b', 'tests': 'c'},
                     'container': 'new-api-dev', 'previous_image_id': 'old', 'previous_version': 'v1', 'candidate_version': 'v2'}
            path = Path(directory) / 'check.json'
            proof['input_fingerprint'] = core.check_binding('existing_database_upgrade', plan)
            release.save(path, proof)
            record = {'schema': 2, 'result': 'passed', 'plan_id': 'id', 'candidate_image_id': 'new',
                      'container': 'new-api-dev', 'checks': [{'name': 'existing_database_upgrade', 'file': 'check.json', 'sha256': core.file_hash(path)}]}
            attach_baseline(directory, plan, proof, record)
            self.assertTrue(core.validate_evidence(plan, record, directory, 'new', 'new'))
            for field, value in [('container', 'empty-db'), ('candidate_image_id', 'other')]:
                with self.assertRaises(core.ReleaseError):
                    core.validate_evidence(plan, dict(record, **{field: value}), directory, 'new', 'new')
            proof.pop('previous_image_id')
            release.save(path, proof)
            record['checks'][0]['sha256'] = core.file_hash(path)
            with self.assertRaises(core.ReleaseError):
                core.validate_evidence(plan, record, directory, 'new', 'new')

    def test_prune_removes_only_this_releases_rehearsal_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = release.Release({})
            runner.directory = Path(directory)
            runner.state = {'validation_target': {'prefix': 'newapi-upgrade-0123456789ab'},
                            'drill_prefixes': ['pricing-drill-abcdef12']}
            (runner.directory / 'pricing-drill-abcdef12-072jx7a7').mkdir()
            (runner.directory / 'gateway-dns-0badc0de-1i8yeccc').mkdir()
            (runner.directory / 'clone').mkdir()
            existing = {
                'container': ['newapi-upgrade-0123456789ab-web', 'pricing-drill-abcdef12-worker',
                              'new-api-dev', 'newapi-check-mysql', 'unrelated'],
                'volume': ['newapi-upgrade-0123456789ab_app-data', 'pricing-drill-abcdef12_mysql-data',
                           'new-api_dev_data', 'newapi-src', 'newapi-gomod'],
                'network': ['newapi-upgrade-0123456789ab_front', 'pricing-drill-abcdef12-front',
                            '1panel-network'],
            }
            removed, commands = [], []

            def fake_list(kind):
                return existing[kind]

            def fake_run(args, **kwargs):
                commands.append(args)
                return ''

            with patch.object(release, 'docker_list', fake_list), patch.object(release, 'run', fake_run):
                runner.prune()
            for args in commands:
                removed += [name for name in args if name.startswith(('newapi-upgrade-0123456789ab',
                                                                       'pricing-drill-abcdef12'))]
            self.assertIn('newapi-upgrade-0123456789ab-web', removed)
            self.assertIn('pricing-drill-abcdef12-worker', removed)
            self.assertIn('newapi-upgrade-0123456789ab_front', removed)
            for keep in ('new-api-dev', 'newapi-check-mysql', 'new-api_dev_data', 'newapi-src',
                         'newapi-gomod', '1panel-network', 'unrelated'):
                self.assertNotIn(keep, removed)
            # Evidence folders for drills are deleted; release records stay.
            self.assertFalse((runner.directory / 'pricing-drill-abcdef12-072jx7a7').exists())
            self.assertFalse((runner.directory / 'gateway-dns-0badc0de-1i8yeccc').exists())
            self.assertTrue((runner.directory / 'clone').exists())
            self.assertTrue(release.read(runner.directory / 'state.json').get('pruned'))

    def test_prune_never_touches_a_named_resource_without_a_release_prefix(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = release.Release({})
            runner.directory = Path(directory)
            runner.state = {'validation_target': {'prefix': 'newapi-upgrade-0123456789ab'}}
            existing = {'container': ['newapi-upgrade-0123456789ab-web', 'newapi-upgrade-other-web'],
                        'volume': [], 'network': []}
            commands = []
            with patch.object(release, 'docker_list', lambda kind: existing[kind]), \
                    patch.object(release, 'run', lambda args, **kwargs: commands.append(args) or ''):
                runner.prune()
            removed = [name for args in commands for name in args if name.startswith('newapi-upgrade-')]
            self.assertEqual(removed, ['newapi-upgrade-0123456789ab-web'])

    def test_failed_prune_never_fails_the_release(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = release.Release({})
            runner.directory = Path(directory)
            runner.state = {'validation_target': {'prefix': 'newapi-upgrade-0123456789ab'}}
            with patch.object(release, 'docker_list', lambda kind: {'container': ['newapi-upgrade-0123456789ab-web'],
                                                                    'volume': [], 'network': []}[kind]), \
                    patch.object(release, 'run', side_effect=release.ReleaseError('daemon busy')):
                runner.prune()
            self.assertFalse(runner.state.get('pruned'))

    def test_completed_stage_is_not_repeated_after_disconnect(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = release.Release({})
            runner.directory = Path(directory)
            runner.state = {'stages': {}}
            action = Mock()
            runner.step('build', action)
            runner.state = release.read(runner.directory / 'state.json')
            runner.step('build', action)
            action.assert_called_once()

    def test_failed_stage_is_never_marked_passed(self):
        with tempfile.TemporaryDirectory() as directory:
            runner = release.Release({})
            runner.directory = Path(directory)
            runner.state = {'stages': {}}
            with self.assertRaises(RuntimeError):
                runner.step('switch', Mock(side_effect=RuntimeError('disconnect')))
            self.assertEqual(release.read(runner.directory / 'state.json')['stages']['switch']['result'], 'failed')

    def test_remote_path_validation(self):
        for path in ('/', '/opt/../etc', '/opt/a;bad'):
            runner = release.Release({'remote_root': path})
            runner.id = 'release-test'
            with self.assertRaises(core.ReleaseError):
                runner.remote_path()

    def test_one_time_state_conversion_backs_up_before_atomic_identity_change(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = remote.rolling.Deployment(directory)
            deployment.save(phase='stable', active='green', worker_image='old')
            previous = dict(deployment.state)
            remote.convert_state(deployment, 'release-test', 'previous-commit')
            self.assertEqual(json.loads((deployment.directory / 'state-before-schema2.json').read_text()), previous)
            self.assertEqual(deployment.state['schema'], 2)
            self.assertEqual(deployment.state['release_revision'], 'previous-commit')
            self.assertEqual(deployment.state['release_operation'], 'release-test')
            remote.convert_state(deployment, 'release-next')
            self.assertEqual(json.loads((deployment.directory / 'state-before-schema2.json').read_text()), previous)

    def test_interrupted_or_unknown_state_cannot_be_converted(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = remote.rolling.Deployment(directory)
            deployment.save(phase='migrating')
            with self.assertRaises(core.ReleaseError):
                remote.convert_state(deployment)
            self.assertFalse((deployment.directory / 'state-before-schema2.json').exists())

    def cancelled_release(self, directory, mode='maintenance', prepared_baseline=True):
        """A maintenance attempt of release-x cancelled before migration, as resume leaves it."""
        baseline_state = {'schema': core.SCHEMA, 'phase': 'stable', 'active': 'green', 'previous': 'blue',
                          'worker_image': 'sha256:old', 'contract': 'c1', 'release_revision': 'base',
                          'release_operation': 'release-before', 'last_maintenance': 'maintenance-' + '1' * 32,
                          'maintenance_step': 'restored', 'migration_started': None, 'maintenance_context': None}
        release_dir = Path(directory) / 'release'
        release_dir.mkdir()
        manifest = {'release_id': 'release-x', 'candidate_image_id': 'sha256:new',
                    'baseline_state_digest': core.digest(baseline_state),
                    'plan': {'mode': mode, 'revision': 'new', 'baseline': 'base', 'backup_strategy': 'zfs_snapshot'}}
        release.save(release_dir / 'manifest.json', manifest)
        prepared = {'manifest_sha256': core.file_hash(release_dir / 'manifest.json')}
        if prepared_baseline:
            prepared['baseline_state'] = baseline_state
        release.save(release_dir / 'prepared.json', prepared)
        deployment = remote.rolling.Deployment(directory)
        deployment.save(**baseline_state)
        deployment.save(release_operation='release-x', previous=None, last_maintenance='maintenance-' + '2' * 32)
        deployment.inspect = Mock(return_value={'Image': 'sha256:old'})
        deployment.maintenance = Mock()
        return deployment, release_dir, manifest

    def test_same_id_retries_maintenance_cancelled_before_migration(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment, release_dir, manifest = self.cancelled_release(directory)
            cancelled = dict(deployment.state)
            self.assertTrue(remote.retryable(deployment, release_dir, manifest))
            with patch.object(remote, 'finish') as finish:
                remote.activate(deployment, release_dir, manifest)
            deployment.maintenance.assert_called_once_with(None, 'sha256:new', backup_strategy='zfs_snapshot')
            finish.assert_called_once()
            evidence = list(release_dir.glob('state-before-retry-*.json'))
            self.assertEqual(len(evidence), 1)
            self.assertEqual(json.loads(evidence[0].read_text()), cancelled)
            self.assertEqual(deployment.state['release_operation'], 'release-x')

    def test_same_id_retry_refuses_anything_but_cancellation_before_migration(self):
        cases = {
            'candidate serving after a disconnected cutover': lambda d: setattr(
                d, 'inspect', Mock(side_effect=lambda name: {'Image': 'sha256:new' if name.endswith('worker') else 'sha256:old'})),
            'migration intent recorded': lambda d: d.save(migration_started=True),
            'maintenance context still open': lambda d: d.save(maintenance_context='maintenance-' + '3' * 32),
            'worker changed': lambda d: d.save(worker_image='sha256:other'),
            'contract changed': lambda d: d.save(contract='c2'),
            'cleanup pending': lambda d: d.save(pending_cleanup='blue'),
            'active slot changed': lambda d: d.save(active='blue'),
            'rollback slot changed': lambda d: d.save(previous='green'),
            'another operation': lambda d: d.save(release_operation='release-other'),
        }
        for name, change in cases.items():
            with self.subTest(name), tempfile.TemporaryDirectory() as directory:
                deployment, release_dir, manifest = self.cancelled_release(directory)
                change(deployment)
                self.assertFalse(remote.retryable(deployment, release_dir, manifest))
                with patch.object(remote, 'finish') as finish, self.assertRaises(core.ReleaseError):
                    remote.activate(deployment, release_dir, manifest)
                deployment.maintenance.assert_not_called()
                finish.assert_not_called()
                self.assertEqual(list(release_dir.glob('state-before-retry-*.json')), [])
        with tempfile.TemporaryDirectory() as directory:
            deployment, release_dir, manifest = self.cancelled_release(directory)
            deployment.save(phase='migrating')
            with self.assertRaisesRegex(core.ReleaseError, 'interrupted'):
                remote.activate(deployment, release_dir, manifest)
            deployment.maintenance.assert_not_called()
        for kwargs in ({'mode': 'blue_green'}, {'prepared_baseline': False}):
            with self.subTest(**kwargs), tempfile.TemporaryDirectory() as directory:
                deployment, release_dir, manifest = self.cancelled_release(directory, **kwargs)
                with self.assertRaisesRegex(core.ReleaseError, 'explicit recovery'):
                    remote.activate(deployment, release_dir, manifest)
                deployment.maintenance.assert_not_called()

    def test_drain_budget_defaults_for_plans_without_grace(self):
        self.assertEqual(remote.drain_budget({'drain_seconds': 120}), (120, remote.rolling.MAINTENANCE_GRACE_SECONDS))
        self.assertEqual(remote.drain_budget({}), (120, 10))
        self.assertEqual(remote.drain_budget({'drain_seconds': 90, 'drain_grace_seconds': 5}), (90, 5))
        for plan in ({'drain_grace_seconds': -1}, {'drain_seconds': '120'}, {'drain_grace_seconds': True}):
            with self.assertRaises(core.ReleaseError):
                remote.drain_budget(plan)
        with patch.object(core, 'git', side_effect=['head', 'baseline', 'service/quota.go', '']), \
                patch.object(core, 'contract_at', return_value=None), \
                patch.object(core, 'fingerprints', return_value={'application': 'a', 'deployment': 'd', 'tests': 't'}):
            plan = core.make_plan('.', 'baseline')
        self.assertEqual(remote.drain_budget(plan), (120, 10))

    def test_recovery_to_old_service_is_not_a_successful_release(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = remote.rolling.Deployment(directory)
            deployment.save(phase='stable', active='green', release_operation='release-test')
            deployment.recover = Mock()
            deployment.inspect = Mock(return_value={'Image': 'old'})
            manifest = {'release_id': 'release-test', 'plan': {'mode': 'maintenance'}, 'candidate_image_id': 'new'}
            with self.assertRaises(core.ReleaseError):
                remote.recover(deployment, Path(directory), manifest)
            self.assertFalse((Path(directory) / 'receipt.json').exists())

    def test_test_changes_do_not_invalidate_database_upgrade_proof(self):
        before = {'fingerprints': {'application': 'a', 'deployment': 'd', 'tests': 't1'}}
        after = {'fingerprints': {'application': 'a', 'deployment': 'd', 'tests': 't2'}}
        self.assertEqual(core.check_binding('existing_database_upgrade', before), core.check_binding('existing_database_upgrade', after))
        self.assertNotEqual(core.check_binding('targeted_tests', before), core.check_binding('targeted_tests', after))

    def test_committing_deleted_entry_does_not_change_build_inputs(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'runtime.go'
            path.write_text('package main')
            with patch.object(core, 'git', return_value='runtime.go\0removed.go'):
                before = core.fingerprints(directory)
            with patch.object(core, 'git', return_value='runtime.go'):
                self.assertEqual(before, core.fingerprints(directory))

    def test_rehearsal_edits_belong_to_test_fingerprint(self):
        for path in ('scripts/maintenance-integration.py', 'scripts/release-ui.cjs',
                     'scripts/backup-integration.py', 'scripts/pricing_equivalence.py'):
            self.assertEqual(core.domain(path), 'tests')

    def test_current_candidate_cannot_masquerade_as_baseline_upgrade(self):
        with tempfile.TemporaryDirectory() as directory:
            plan = {'id': 'id', 'checks': ['existing_database_upgrade'],
                    'fingerprints': {'application': 'a', 'deployment': 'b', 'tests': 'c'}}
            proof = {'result': 'passed', 'candidate_image_id': 'new', 'previous_image_id': 'new',
                     'baseline_image_id': 'old', 'previous_version': 'v2', 'candidate_version': 'v2',
                     'container': 'new-api-dev', 'input_fingerprint': core.check_binding('existing_database_upgrade', plan)}
            path = Path(directory) / 'upgrade.json'
            release.save(path, proof)
            record = {'schema': 2, 'result': 'passed', 'plan_id': 'id', 'container': 'new-api-dev',
                      'candidate_image_id': 'new', 'checks': [{'name': 'existing_database_upgrade',
                          'file': path.name, 'sha256': core.file_hash(path)}]}
            attach_baseline(directory, plan, proof, record)
            with self.assertRaises(core.ReleaseError):
                core.validate_evidence(plan, record, directory, 'new', 'new')


class ContainerLogTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.info = {'Id': 'container-id', 'Image': 'old-image',
                     'State': {'Running': False, 'FinishedAt': 'stopped-at'},
                     'HostConfig': {'LogConfig': {'Type': 'json-file'}}}

    def capture(self, argv, **kwargs):
        self.assertEqual(argv, ['docker', 'logs', '--timestamps', 'container-id'])
        kwargs['stdout'].write(b'2026-09-25 stdout\x00\xff\n')
        kwargs['stderr'].write(b'2026-09-25 stderr\r\n')
        return Mock(returncode=0)

    def archive(self):
        with patch.object(local, 'run', return_value=json.dumps([self.info])), \
                patch.object(local.subprocess, 'run', side_effect=self.capture):
            return local.archive_container_logs(self.directory)

    def test_preserves_both_streams_and_rechecks_complete_bytes(self):
        archive = self.archive()
        self.assertEqual(archive['container_id'], 'container-id')
        self.assertEqual(len(archive['files']), 4)
        for name, expected in archive['files'].items():
            self.assertEqual(core.file_hash(self.directory / name), expected)
        with patch.object(local, 'run', return_value=json.dumps([self.info])):
            local.verify_container_logs(self.directory, archive)

    def test_capture_failure_retains_partial_logs(self):
        def fail(argv, **kwargs):
            self.capture(argv, **kwargs)
            return Mock(returncode=1)
        with patch.object(local, 'run', return_value=json.dumps([self.info])), \
                patch.object(local.subprocess, 'run', side_effect=fail):
            with self.assertRaises(core.ReleaseError):
                local.archive_container_logs(self.directory)
        self.assertEqual(len(list(self.directory.glob('*.log'))), 2)

    def test_running_container_cannot_be_archived(self):
        self.info['State']['Running'] = True
        with patch.object(local, 'run', return_value=json.dumps([self.info])), \
                patch.object(local.subprocess, 'run') as capture:
            with self.assertRaises(core.ReleaseError):
                local.archive_container_logs(self.directory)
            capture.assert_not_called()

    def test_modified_archive_blocks_recreation(self):
        archive = self.archive()
        (self.directory / next(iter(archive['files']))).write_bytes(b'tampered')
        with patch.object(local, 'run', return_value=json.dumps([self.info])):
            with self.assertRaises(core.ReleaseError):
                local.verify_container_logs(self.directory, archive)

    def test_replaced_or_restarted_container_invalidates_archive(self):
        archive = self.archive()
        for info in (dict(self.info, Id='replacement'),
                     dict(self.info, State={'Running': False, 'FinishedAt': 'later'})):
            with patch.object(local, 'run', return_value=json.dumps([info])):
                with self.assertRaises(core.ReleaseError):
                    local.verify_container_logs(self.directory, archive)

    def test_backup_gate_checks_actual_archive_bytes(self):
        archive = self.archive()
        for name in ('database.sql', 'data.tar.gz'):
            (self.directory / name).write_bytes(b'backup fixture')
        plan = {'id': 'plan', 'checks': ['backup'],
                'fingerprints': {'application': 'a', 'deployment': 'd', 'tests': 't'}}
        proof = {'result': 'passed', 'candidate_image_id': 'new', 'previous_image_id': 'old-image',
                 'input_fingerprint': core.check_binding('backup', plan), 'container_logs': archive,
                 'files': {n: core.file_hash(self.directory / n) for n in ('database.sql', 'data.tar.gz')} | archive['files']}
        release.save(self.directory / 'backup.json', proof)
        record = {'schema': 2, 'result': 'passed', 'plan_id': 'plan', 'candidate_image_id': 'new',
                  'container': 'new-api-dev', 'checks': [{'name': 'backup', 'file': 'backup.json',
                    'sha256': core.file_hash(self.directory / 'backup.json')}]}
        attach_baseline(self.directory, plan, proof, record, old='old-image')
        self.assertTrue(core.validate_evidence(plan, record, self.directory, 'new', 'new'))
        (self.directory / next(iter(archive['files']))).write_bytes(b'changed')
        with self.assertRaises(core.ReleaseError):
            core.validate_evidence(plan, record, self.directory, 'new', 'new')


class DockerUITests(unittest.TestCase):
    def setUp(self):
        prefix = 'newapi-upgrade-0123456789ab'
        self.state = {'id': 'release-ui-test', 'candidate_image_id': 'candidate', 'baseline_generation': 'base',
                      'plan': {'mode': 'maintenance'}, 'baseline_state': {'worker_image': 'old'},
                      'validation_target': {'prefix': prefix, 'owner': 'release-ui-test', 'baseline_generation': 'base',
                                            **{role: prefix + '-' + role for role in ('web', 'worker', 'mysql')}}}
        self.objects = {prefix + '-' + role: {'Name': '/' + prefix + '-' + role, 'State': {'Running': True},
            'Image': 'candidate', 'Config': {'Labels': {'cc.e-flowcode.validation-owner': 'release-ui-test',
                                                       'cc.e-flowcode.validation-stack': prefix},
                                             'Env': ['MYSQL_ROOT_PASSWORD=synthetic']},
            'NetworkSettings': {'Networks': {prefix + '_runtime': {}}}} for role in ('web', 'worker', 'mysql', 'gateway')}

    def test_ui_accepts_owned_candidate_clone_and_keeps_credentials_out_of_argv(self):
        result = ui.validate_target(self.state, self.objects.__getitem__)
        self.assertEqual(result['mysql']['password'], 'synthetic')
        self.assertEqual(result['origin'], 'http://newapi-upgrade-0123456789ab-gateway:3000')

    def test_ui_rejects_foreign_mysql_owner_and_wrong_candidate(self):
        name = self.state['validation_target']['mysql']
        self.objects[name]['Config']['Labels']['cc.e-flowcode.validation-owner'] = 'another-release'
        with self.assertRaises(core.ReleaseError):
            ui.validate_target(self.state, self.objects.__getitem__)
        self.setUp()
        self.objects[self.state['validation_target']['web']]['Image'] = 'wrong'
        with self.assertRaises(core.ReleaseError):
            ui.validate_target(self.state, self.objects.__getitem__)

    def test_ui_expects_candidate_worker_after_handoff(self):
        worker = self.objects[self.state['validation_target']['worker']]
        self.state['plan'] = {'mode': 'blue_green', 'worker': 'retain'}
        with self.assertRaises(core.ReleaseError):
            ui.validate_target(self.state, self.objects.__getitem__)
        self.state['plan']['worker'] = 'handoff'
        ui.validate_target(self.state, self.objects.__getitem__)
        worker['Image'] = 'old'
        with self.assertRaises(core.ReleaseError):
            ui.validate_target(self.state, self.objects.__getitem__)

    def test_ui_rejects_dev_and_wrong_network(self):
        self.state['validation_target']['prefix'] = 'new-api-dev'
        with self.assertRaises(core.ReleaseError):
            ui.validate_target(self.state, self.objects.__getitem__)
        self.setUp()
        self.objects[self.state['validation_target']['mysql']]['NetworkSettings']['Networks'] = {'foreign': {}}
        with self.assertRaises(core.ReleaseError):
            ui.validate_target(self.state, self.objects.__getitem__)


class BaselineTests(unittest.TestCase):
    def test_rehearsals_share_explicit_nonoverlapping_subnet_policy(self):
        responses = ['owned-network', json.dumps([{'IPAM': {'Config': [{'Subnet': '198.19.0.0/28'}]}}])]
        with patch.dict(os.environ, {'NEW_API_VALIDATION_SUBNET_POOL': '198.19.0.0/16'}), patch.object(baseline, 'run', side_effect=responses):
            result = baseline.validation_networks({'runtime': {'internal': True}, 'front': {}, 'external': {'external': True}})
        self.assertEqual(result['runtime']['ipam']['config'][0]['subnet'], '198.19.0.16/28')
        self.assertEqual(result['front']['ipam']['config'][0]['subnet'], '198.19.0.32/28')
        self.assertEqual(result['external'], {'external': True})

    def test_migrated_baseline_requires_and_preserves_price_provenance(self):
        for filename in ('baseline-snapshot.py', 'pricing-snapshot.py'):
            spec = importlib.util.spec_from_file_location('pricing_capture_test', Path(__file__).with_name(filename))
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            pricing = {'ModelPricing': '{"version":2,"task_priority_version":1,"models":{}}'}
            with self.assertRaisesRegex(RuntimeError, 'provenance'):
                module.sanitize_migration_provenance(pricing)
            record = {'source_hash': 'source', 'target_hash': 'target', 'original': '{"version":2,"models":{}}',
                      'entries': [{'model': 'm', 'channel_id': 153, 'status': 'ready'}]}
            pricing['ModelPricingTaskMigrationV1'] = json.dumps(record)
            result = module.sanitize_migration_provenance(pricing)
            saved = json.loads(result['ModelPricingTaskMigrationV1'])
            self.assertEqual(saved['original'], record['original'])
            self.assertEqual(saved['source_hash'], 'source')
            self.assertNotIn('channel_id', saved['entries'][0])

    def test_planned_contract_binds_application_and_deployment(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            app = root / 'internal/deployment/lifecycle.go'
            app.parent.mkdir(parents=True)
            app.write_text('const Contract = "v8"\n')
            controller = root / 'scripts/rolling-deploy.py'
            controller.parent.mkdir()
            controller.write_text('CONTRACT = "v8"\n')
            self.assertEqual(core.application_contract(root), 'v8')
            controller.write_text('CONTRACT = "v7"\n')
            with self.assertRaises(core.ReleaseError):
                core.application_contract(root)

    def test_validation_subnets_skip_overlaps_and_do_not_expand_exhausted_pool(self):
        self.assertEqual(baseline.select_validation_subnets('198.19.0.0/24', ['198.19.0.0/27']),
                         ['198.19.0.32/28', '198.19.0.48/28'])
        with self.assertRaisesRegex(core.ReleaseError, 'exhausted'):
            baseline.select_validation_subnets('198.19.0.0/27', ['198.19.0.0/28'])
        for pool in ('0.0.0.0/0', '8.8.0.0/16'):
            with self.assertRaises(core.ReleaseError):
                baseline.select_validation_subnets(pool, [])

    def test_explicit_subnets_accept_docker_host_network_null_ipam(self):
        with tempfile.TemporaryDirectory() as folder, patch.object(baseline, 'run', side_effect=[
                'host-id', json.dumps([{'IPAM': {'Config': None}}])]):
            stack = baseline.Stack.create(folder, 'newapi-upgrade-0123456789ab', 'old', 'old',
                'v7', 'release-test', subnet_pool='198.19.0.0/24')
            config = release.read(stack.path)
            self.assertTrue(config['networks']['runtime']['internal'])
            self.assertEqual(config['networks']['runtime']['ipam']['config'], [{'subnet': '198.19.0.0/28'}])
            self.assertEqual(config['networks']['front']['ipam']['config'], [{'subnet': '198.19.0.16/28'}])

    def test_seed_empty_mapping_is_a_string_not_an_invalid_hex_literal(self):
        self.assertEqual(baseline.value_sql(''), "''")
        self.assertEqual(baseline.value_sql(None), 'NULL')
        self.assertEqual(baseline.value_sql("'"), 'CONVERT(0x27 USING utf8mb4)')

    def test_task_snapshot_retains_paused_and_dangling_bindings_without_source_ids(self):
        spec = importlib.util.spec_from_file_location('snapshot', Path(__file__).with_name('baseline-snapshot.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        captured = {'channels': [{'id': 74, 'type': 1, 'status': 2, 'models': 'gpt',
                                  'model_mapping': '{"gpt":"other"}', 'group': 'vip'}],
                    'bindings': [{'channel_id': 153, 'key': 'sora', 'enabled': False}],
                    'control': [{'revision': 4, 'enabled': False}], 'plugins': [], 'sources': []}
        result = module.sanitize_task_pricing(captured)
        self.assertEqual(result['channels'][0]['id'], 1000)
        self.assertEqual(result['bindings'][0]['channel_id'], 1001)
        self.assertEqual(result['channels'][0]['status'], 2)
        self.assertEqual(result['channels'][0]['model_mapping'], '{"gpt":"other"}')
        self.assertEqual(captured['channels'][0]['id'], 74)

    def test_fixed_source_must_match_reviewed_deployed_blob(self):
        import hashlib
        source = b'fixed reviewed source\n'
        checksum = hashlib.sha256(source).hexdigest()
        row = {'key': 'happyhorse', 'version': '1', 'source_hash': checksum, 'api_version': 1}
        snapshot = {'task_pricing': {'schema': 1, 'control': [{}], 'bindings': [row],
                                    'plugins': [], 'sources': [dict(row, actual_source_hash=checksum)]}}
        with patch.object(baseline.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=source)):
            self.assertEqual(baseline.resolve_task_sources(snapshot, 'reviewed')[checksum]['source'], source.decode())
        with patch.object(baseline.subprocess, 'run', return_value=SimpleNamespace(returncode=0, stdout=b'changed')):
            with self.assertRaisesRegex(core.ReleaseError, 'reviewed Git blob'):
                baseline.resolve_task_sources(snapshot, 'reviewed')
        snapshot['task_pricing']['sources'][0]['actual_source_hash'] = 'bad'
        with self.assertRaisesRegex(core.ReleaseError, 'hash mismatch'):
            baseline.resolve_task_sources(snapshot, 'reviewed')

    def test_seed_keeps_complete_task_topology_with_synthetic_credentials(self):
        task = {'channels': [{'id': 1000, 'type': 17, 'status': 2, 'models': 'wan',
                             'model_mapping': '{"wan":"wan-v2"}', 'group': 'vip'}],
                'control': [{'revision': 9, 'enabled': 0, 'factory_enabled': 1, 'disabled_factory': '["alibaba"]'}],
                'plugins': [], 'bindings': [{'channel_id': 1000, 'key': 'alibaba', 'enabled': 0}]}
        seed = baseline.seed_sql({'setup': {'version': 'v7', 'initialized_at': 123}, 'pricing': {},
            'task_pricing': task, 'samples': {key: [] for key in ('users', 'tokens', 'logs', 'tasks')}}, 'hash').decode()
        self.assertEqual(seed.count('INSERT INTO `channels`'), 1)
        self.assertIn('`model_mapping`', seed)
        self.assertIn('INSERT INTO `task_plugin_production_bindings`', seed)
        self.assertIn('INSERT INTO `task_plugin_controls`', seed)
        self.assertIn('local-mock-only'.encode().hex(), seed)
        self.assertNotIn('baseline-sample-channel'.encode().hex(), seed)

    def test_amount_expression_migration_preserves_prices(self):
        expression = 'v1:tier("video", ceil(param("seconds")) * 10000)'
        before = {'ModelPricing': {'version': 1, 'models': {
            'video': {'expression': expression, 'unit_price': 0},
            'inactive': {'expression': 'tier("zero", 0)'}}}, 'GroupRatio': {'vip': 0.125}}
        after = {'ModelPricing': {'version': 2, 'models': {
            'video': {'expression': 'v2:(' + expression[3:] + '\n) / 1000000', 'unit_price': 0},
            'inactive': {'expression': 'v2:(tier("zero", 0)\n) / 1000000'}}}, 'GroupRatio': {'vip': 0.125}}
        proof = compare_pricing(before, after)
        self.assertEqual(len(proof['amount_unit_changes']), 2)
        self.assertEqual(compare_pricing(after, after)['amount_unit_changes'], [])
        for bad in ('v2:0.01', 'v2:(' + expression[3:] + '\n) / 1000',
                    'v2:(' + expression[3:] + '\n) / 1000000 + 1'):
            changed = json.loads(json.dumps(after))
            changed['ModelPricing']['models']['video']['expression'] = bad
            with self.assertRaises(AssertionError):
                compare_pricing(before, changed)

    def test_baseline_rejects_missing_or_invented_initialization_state(self):
        for record in (None, {}, {'version': 'v7', 'initialized_at': 0},
                       {'version': '', 'initialized_at': 123}, {'version': 'v7', 'initialized_at': True}):
            with self.subTest(record=record), self.assertRaises(core.ReleaseError):
                baseline.seed_sql({'setup': record}, 'unused')

    def test_root_user_alone_does_not_satisfy_initialized_readiness(self):
        for response in ({'success': True, 'data': {'status': False, 'root_init': True}},
                         {'success': False, 'data': {'status': True}}, {'success': True, 'data': {}}):
            with self.subTest(response=response), self.assertRaises(core.ReleaseError):
                baseline.require_initialized(response)
        baseline.require_initialized({'success': True, 'data': {'status': True, 'root_init': False}})

    def test_capture_and_cutover_observe_the_same_authorization_inputs(self):
        modules = []
        for name in ('baseline-snapshot', 'pricing-snapshot'):
            spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(name + '.py'))
            module = importlib.util.module_from_spec(spec)
            spec.loader.exec_module(module)
            modules.append(module)
        captured, cutover = set(modules[0].PRICING_KEYS), set(modules[1].KEYS)
        self.assertEqual(captured, cutover)
        self.assertTrue({'UserUsableGroups', 'group_ratio_setting.group_special_usable_group',
                         'AutoGroups', 'DefaultUseAutoGroup'} <= captured)

    def test_rehearsal_identity_uses_existing_authorization_without_mutation(self):
        options = {'GroupRatio': '{"media":1.3}', 'UserUsableGroups': '{}',
                   'group_ratio_setting.group_special_usable_group': '{"vip":{"+:media":"media"}}'}
        original = dict(options)
        self.assertEqual(baseline.rehearsal_user_tier(options, 'media'), 'vip')
        self.assertEqual(options, original)
        options['UserUsableGroups'] = '{"media":"media"}'
        self.assertEqual(baseline.rehearsal_user_tier(options, 'media'), 'default')

    def test_rehearsal_identity_rejects_unauthorized_or_unpriced_groups(self):
        options = {'GroupRatio': '{"media":1.3}', 'UserUsableGroups': '{"media":"media"}',
                   'group_ratio_setting.group_special_usable_group': '{"default":{"-:media":"removed"}}'}
        with self.assertRaises(core.ReleaseError):
            baseline.rehearsal_user_tier(options, 'media')
        with self.assertRaises(core.ReleaseError):
            baseline.rehearsal_user_tier(options, 'missing')
        with self.assertRaises(core.ReleaseError):
            baseline.rehearsal_user_tier({'GroupRatio': options['GroupRatio']}, 'media')

    def test_rehearsal_identity_does_not_choose_ambiguous_aliases(self):
        options = {'GroupRatio': '{"media":1}', 'UserUsableGroups': '{}',
                   'group_ratio_setting.group_special_usable_group': '{"vip":{"+:media":"yes","-:media":"no"}}'}
        with self.assertRaises(core.ReleaseError):
            baseline.rehearsal_user_tier(options, 'media')

    def test_plugin_pricing_equivalence_preserves_every_override(self):
        before = {'billing_setting.task_expressions': {'alias': {'alibaba': 'v2:0.1', 'sora': 'v2:0'}}}
        after = {'ModelPricing': {'version': 2, 'models': {'alias': {'task_expressions': before['billing_setting.task_expressions']['alias']}}}}
        self.assertTrue(compare_pricing(before, after)['prices_and_groups_preserved'])
        for rules in ({'alibaba': 'v2:0.2', 'sora': 'v2:0'}, {'alibaba': 'v2:0.1'}, {}):
            with self.assertRaises(AssertionError):
                compare_pricing(before, {'ModelPricing': {'version': 2, 'models': {'alias': {'task_expressions': rules}}}})

    def test_task_rule_format_equivalence_is_exact(self):
        before = {'ModelPricing': {'version': 2, 'models': {'alias': {'task_expressions': {'sora': 'v2:0.1'}}}}}
        rule = {'version': 1, 'kind': 'amount', 'expression': 'v2:0.1'}
        after = {'ModelPricing': {'version': 2, 'models': {'alias': {'task_expressions': {'sora': rule}}}}}
        self.assertTrue(compare_pricing(before, after)['prices_and_groups_preserved'])
        for changed in ({**rule, 'expression': 'v2:0.2'}, {**rule, 'version': 2}, {**rule, 'unit_price': 0.1}):
            with self.assertRaises(AssertionError):
                compare_pricing(before, {'ModelPricing': {'version': 2, 'models': {'alias': {'task_expressions': {'sora': changed}}}}})
        after['ModelPricing']['task_priority_version'] = 1
        with self.assertRaisesRegex(AssertionError, 'priority'):
            compare_pricing(before, after)

    def test_pricing_equivalence_rejects_changed_authorization_and_routing(self):
        for key, value in {'UserUsableGroups': {'media': 'Media'},
                           'group_ratio_setting.group_special_usable_group': {'vip': {'media': 'Media'}},
                           'AutoGroups': ['media'], 'DefaultUseAutoGroup': True}.items():
            with self.subTest(key=key):
                before = {'ModelPricing': {'models': {}}, key: value}
                self.assertTrue(compare_pricing(before, dict(before))['group_authorization_preserved'])
                with self.assertRaisesRegex(AssertionError, 'authorization/routing'):
                    compare_pricing(before, {'ModelPricing': {'models': {}}})

    def test_compatible_clone_keeps_actual_old_worker(self):
        for mode in ('blue_green', 'components'):
            roles = baseline.candidate_roles(mode, 'candidate', 'different-old-worker')
            self.assertEqual(roles['worker'], 'different-old-worker')
            self.assertEqual(roles['web'], 'candidate')
        self.assertEqual(set(baseline.candidate_roles('maintenance', 'candidate', 'old').values()), {'candidate'})

    def test_seed_keeps_prices_and_replaces_all_identities(self):
        snapshot = {'setup': {'version': 'original-installation-version', 'initialized_at': 1700000042},
            'task_pricing': {'channels': [{'id': 1000, 'type': 1, 'status': 2, 'models': 'sample', 'group': 'default'}],
                'bindings': [], 'plugins': [], 'control': [{'revision': 1, 'enabled': 0,
                    'factory_enabled': 1, 'disabled_factory': '[]'}]},
            'pricing': {'ModelPricing': '{"models":{}}', 'GroupRatio': '{"default":1.3}',
                               'UserUsableGroups': '{"media":"Media"}',
                               'group_ratio_setting.group_special_usable_group': '{"vip":{"+:media":"Media"}}'},
            'samples': {'users': [{'quota': -10, 'used_quota': 13, 'request_count': 2, 'status': 1}],
                        'tokens': [{'remain_quota': 12, 'used_quota': 3, 'status': 1, 'unlimited_quota': 0}],
                        'logs': [{'quota': 9, 'prompt_tokens': 10, 'completion_tokens': 2, 'type': 2}],
                        'tasks': [{'quota': 4, 'status': 'SUCCESS', 'platform': 'suno'}]}}
        text = baseline.seed_sql(snapshot, 'synthetic-bcrypt').decode()
        self.assertIn('INSERT INTO `setups`', text)
        self.assertIn(baseline.value_sql('original-installation-version'), text)
        self.assertIn('1700000042', text)
        self.assertIn(baseline.value_sql(snapshot['pricing']['GroupRatio']), text)
        for key in ('UserUsableGroups', 'group_ratio_setting.group_special_usable_group'):
            self.assertIn(baseline.value_sql(key), text)
            self.assertIn(baseline.value_sql(snapshot['pricing'][key]), text)
        self.assertIn('-10', text)
        self.assertIn(baseline.value_sql('http://upstream:8080'), text)
        self.assertIn(baseline.value_sql('local-sample-100'), text)
        self.assertNotIn('DELETE', text)
        self.assertNotIn('TRUNCATE', text)

    def test_corrupt_frozen_generation_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / 'database.sql'
            path.write_bytes(b'baseline')
            manifest = {'result': 'passed', 'scope': baseline.SCOPE, 'files': {path.name: core.file_hash(path)}}
            baseline.verify_files(folder, manifest)
            path.write_bytes(b'candidate data')
            with self.assertRaises(core.ReleaseError):
                baseline.verify_files(folder, manifest)

    def test_stack_uses_fresh_owned_resources_and_internal_network(self):
        with tempfile.TemporaryDirectory() as folder:
            stack = baseline.Stack.create(folder, 'newapi-upgrade-0123456789ab', 'old-web', 'old-worker', 'v7', 'release-test')
            config = release.read(stack.path)
            self.assertTrue(config['networks']['runtime']['internal'])
            self.assertEqual(config['services']['web']['image'], 'old-web')
            self.assertEqual(config['services']['worker']['image'], 'old-worker')
            self.assertNotIn('front', config['services']['web']['networks'])
            self.assertNotIn('external', str(config['volumes']))
            self.assertNotIn('new-api-dev', json.dumps(config))
            with patch.object(baseline, 'run', return_value=json.dumps([{'Config': {'Labels': {}}}])):
                with self.assertRaises(core.ReleaseError):
                    stack.inspect(stack.spec['web'])

    def test_dev_is_never_accepted_as_clone(self):
        with self.assertRaises(core.ReleaseError):
            baseline.Stack.create('.', 'new-api-dev', 'old', 'old', 'v7', 'release-test')

    def test_baseline_from_other_production_revision_is_rejected(self):
        with tempfile.TemporaryDirectory() as folder:
            plan = {'id': 'plan', 'checks': [], 'baseline': 'different'}
            record = {'schema': 2, 'result': 'passed', 'plan_id': 'plan', 'candidate_image_id': 'candidate',
                      'container': 'newapi-upgrade-0123456789ab-web', 'validation_contract': 'baseline-clone-v1',
                      'baseline_generation': 'one'}
            release.save(Path(folder) / 'baseline.json', {'id': 'one', 'result': 'passed', 'scope': baseline.SCOPE,
                                                        'production': {'revision': 'original'}})
            record['baseline_manifest_sha256'] = core.file_hash(Path(folder) / 'baseline.json')
            with self.assertRaises(core.ReleaseError):
                core.validate_evidence(plan, record, folder, 'candidate', 'candidate')


class SnapshotBackupTests(unittest.TestCase):
    TARGET = {'dataset': 'data/vmdata/vm-100-disk-1', 'fs_uuid': 'uuid-1', 'mount': '/srv/mysql'}

    def drill_root(self, directory, **evidence):
        root = Path(directory)
        (root / 'scripts').mkdir()
        for name in core.SNAPSHOT_BOUND_FILES:
            (root / name).write_text(name, encoding='utf-8')
        record = {'result': 'passed', 'finished': 1000.0, 'target': dict(self.TARGET),
                  'files': {name: core.file_hash(root / name) for name in core.SNAPSHOT_BOUND_FILES}}
        record.update(evidence)
        (root / '.releases').mkdir()
        release.save(root / core.SNAPSHOT_DRILL_EVIDENCE, record)
        return root

    def test_configuration_alone_never_grants_snapshot_backup(self):
        config = {'backup_strategy': 'zfs_snapshot', 'snapshot_backup': dict(self.TARGET)}
        with tempfile.TemporaryDirectory() as directory:
            self.assertEqual(core.backup_strategy(directory, config, now=1000)[0], 'stopped_full')
            self.assertEqual(core.backup_strategy(directory, {}, now=1000), ('stopped_full', None))
            with self.assertRaises(core.ReleaseError):
                core.backup_strategy(directory, {'backup_strategy': 'binlog'})

    def test_snapshot_backup_requires_fresh_matching_drill_of_the_same_tools(self):
        config = {'backup_strategy': 'zfs_snapshot', 'snapshot_backup': dict(self.TARGET)}
        with tempfile.TemporaryDirectory() as directory:
            root = self.drill_root(directory)
            self.assertEqual(core.backup_strategy(root, config, now=1000), ('zfs_snapshot', None))
            self.assertEqual(core.backup_strategy(root, config, now=1000 + core.SNAPSHOT_DRILL_MAX_AGE + 1)[0], 'stopped_full')
            other = {**config, 'snapshot_backup': {**self.TARGET, 'fs_uuid': 'uuid-2'}}
            self.assertEqual(core.backup_strategy(root, other, now=1000)[0], 'stopped_full')
            (root / 'scripts/rolling-deploy.py').write_text('edited', encoding='utf-8')
            strategy, reason = core.backup_strategy(root, config, now=1000)
            self.assertEqual(strategy, 'stopped_full')
            self.assertIn('rolling-deploy.py', reason)
        with tempfile.TemporaryDirectory() as directory:
            root = self.drill_root(directory, result='failed')
            self.assertEqual(core.backup_strategy(root, config, now=1000)[0], 'stopped_full')

    def test_only_maintenance_plans_carry_the_snapshot_strategy(self):
        with patch.object(core, 'git', side_effect=['head', 'baseline', 'service/quota.go', '']), \
                patch.object(core, 'contract_at', return_value=None), \
                patch.object(core, 'fingerprints', return_value={'application': 'a', 'deployment': 'd', 'tests': 't'}), \
                patch.object(core, 'backup_strategy', return_value=('zfs_snapshot', None)):
            self.assertEqual(core.make_plan('.', 'baseline', {})['backup_strategy'], 'zfs_snapshot')
        with patch.object(core, 'git', side_effect=['head', 'baseline', 'web/src/main.tsx', '']), \
                patch.object(core, 'contract_at', return_value=core.application_contract('.')), \
                patch.object(core, 'fingerprints', return_value={'application': 'a', 'deployment': 'd', 'tests': 't'}), \
                patch.object(core, 'backup_strategy', return_value=('zfs_snapshot', None)):
            self.assertEqual(core.make_plan('.', 'baseline', {})['backup_strategy'], 'stopped_full')

    def snapshot_deployment(self, directory):
        deployment = remote.rolling.Deployment(directory)
        deployment.check_snapshot_target = Mock(return_value={**self.TARGET})
        return deployment

    def test_snapshot_always_thaws_even_when_the_guard_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = self.snapshot_deployment(directory)
            deployment.snapshot_guard = Mock(side_effect=remote.rolling.DeployError('guard refused snapshot'))
            with patch.object(remote.rolling, 'run') as run:
                with self.assertRaises(remote.rolling.DeployError):
                    deployment.snapshot_database('maint-' + '0' * 32)
            calls = [c.args for c in run.call_args_list]
            self.assertIn(('fsfreeze', '--freeze', '/srv/mysql'), calls)
            self.assertEqual(calls[-1], ('fsfreeze', '--unfreeze', '/srv/mysql'))

    def test_snapshot_maintenance_backup_is_bound_to_the_recorded_snapshot(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = self.snapshot_deployment(directory)
            context = deployment.directory / ('maintenance-' + 'a' * 32)
            context.mkdir()
            remote.rolling.atomic_json(context / 'original.json', {'config': {'services': {'worker': {'environment': {}}}}})
            deployment.save(maintenance_context=context.name, maintenance_backup_strategy='zfs_snapshot')
            deployment.snapshot_guard = Mock(return_value={'ok': True, 'name': 'maint-' + 'a' * 32, 'guid': '42'})
            deployment.capture_backup = Mock(side_effect=lambda target, args: target.write_bytes(b'tar'))
            with patch.object(remote.rolling, 'run'):
                deployment.backup_maintenance()
            manifest = json.loads((context / 'backup.json').read_text())
            self.assertEqual(manifest['strategy'], 'zfs_snapshot')
            self.assertEqual({f['file'] for f in manifest['files']}, {'original.json', 'snapshot.json', 'data.tar.gz'})
            self.assertEqual(deployment.snapshot_guard.call_args_list[0].args, ('snapshot', 'maint-' + 'a' * 32))
            deployment.snapshot_guard = Mock(return_value={'ok': True, 'guid': '42'})
            deployment.verify_maintenance_backup()
            deployment.snapshot_guard = Mock(return_value={'ok': True, 'guid': 'replaced'})
            with self.assertRaises(remote.rolling.DeployError):
                deployment.verify_maintenance_backup()
            deployment.save(maintenance_backup_strategy='stopped_full')
            with self.assertRaises(remote.rolling.DeployError):
                deployment.verify_maintenance_backup()

    def test_unknown_strategy_is_rejected_before_admission_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            deployment = remote.rolling.Deployment(directory)
            deployment.require_stable = Mock()
            with self.assertRaises(remote.rolling.DeployError):
                deployment.maintenance(None, 'image', backup_strategy='binlog')
            deployment.require_stable.assert_not_called()


if __name__ == '__main__':
    unittest.main()
