import tempfile
from pathlib import Path
import unittest

from backup_chain import verify_chain, archive_command, snapshot_command
from release_core import ReleaseError, digest, file_hash


class BackupChainTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'snapshot.sql').write_bytes(b'completed snapshot')
        (self.root / 'binlog.000007').write_bytes(b'\xfebin' + b'0' * 20)
        self.manifest = {'schema': 1, 'binlog_format': 'ROW', 'server_uuid': 'source', 'databases': ['new_api'],
            'snapshot_position': {'file': 'binlog.000007', 'position': 4},
            'cutoff_position': {'file': 'binlog.000007', 'position': 24},
            'snapshot': self.entry('snapshot.sql'), 'binlogs': [self.entry('binlog.000007')]}
        self.proof = {'result': 'passed', 'chain_sha256': digest(self.manifest), 'server_uuid': 'source',
            'cutoff_position': self.manifest['cutoff_position'], 'isolated_resources_cleaned': True,
            'schema_and_data_match': True, 'application_restart_passed': True, 'verified_at': 100}

    def entry(self, name):
        return {'file': name, 'bytes': (self.root / name).stat().st_size, 'sha256': file_hash(self.root / name)}

    def test_complete_chain_requires_real_restore(self):
        self.assertEqual(verify_chain(self.root, self.manifest, self.proof, now=110)['result'], 'passed')
        with self.assertRaises(ReleaseError):
            verify_chain(self.root, self.manifest, now=110)

    def test_missing_truncated_or_changed_log_refuses_release(self):
        for mutation in ('gap', 'truncated', 'changed'):
            with self.subTest(mutation=mutation):
                if mutation == 'gap':
                    self.manifest['binlogs'] = []
                elif mutation == 'truncated':
                    self.manifest['binlogs'] = [self.entry('binlog.000007')]
                    self.manifest['cutoff_position']['position'] = 999
                else:
                    (self.root / 'binlog.000007').write_bytes(b'changed')
                with self.assertRaises(ReleaseError):
                    verify_chain(self.root, self.manifest, self.proof, now=110)

    def test_no_credentials_in_argv_and_no_guessing_start(self):
        cmd = archive_command('client.cnf', self.root, 'binlog.000007')
        self.assertIn('--stop-never', cmd)
        self.assertFalse(any('password=' in value for value in cmd))
        with self.assertRaises(ReleaseError):
            archive_command('client.cnf', self.root, '../binlog.000007')
        self.assertIn('--source-data=2', snapshot_command('client.cnf', ['new_api']))


if __name__ == '__main__':
    unittest.main()
