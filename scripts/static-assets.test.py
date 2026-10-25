"""Extraction and archive safety contracts; run inside the release tools image."""
import hashlib
from pathlib import Path
import struct
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from static_assets import AssetError, embedded_files, immutable_path, retain_container


def elf_fixture(content=b'console.log("legacy");' * 100):
    entries = [('web/', None), ('web/dist/', None),
               ('web/dist/index.html', b'<html>fixture</html>'),
               ('web/dist/static/', None), ('web/dist/static/js/', None),
               ('web/dist/static/js/test.1234567890.js', content)]
    binary = bytearray(32768)
    binary[:6] = b'\x7fELF\x02\x01'
    struct.pack_into('<Q', binary, 32, 64)
    struct.pack_into('<HH', binary, 54, 56, 1)
    base, table, cursor = 0x400000, 512, 4096
    struct.pack_into('<IIQQQQQQ', binary, 64, 1, 4, 0, base, base, len(binary), len(binary), 4096)
    struct.pack_into('<QQQ', binary, table - 24, base + table, len(entries), len(entries))
    for i, (name, data) in enumerate(entries):
        encoded = name.encode()
        name_ptr = base + cursor
        binary[cursor:cursor + len(encoded)] = encoded
        cursor += len(encoded)
        data_ptr, length, checksum = 0, 0, bytes(16)
        if data is not None:
            data_ptr, length = base + cursor, len(data)
            binary[cursor:cursor + length] = data
            cursor += length
            checksum = bytearray(hashlib.sha256(data if length <= 1024 else b'\x01' + data).digest()[:16])
            if length <= 1024:
                checksum[0] ^= 0xff
        struct.pack_into('<QQQQ16s', binary, table + i * 48, name_ptr, len(encoded), data_ptr, length, bytes(checksum))
    return bytes(binary)


class StaticAssetsTests(unittest.TestCase):
    def test_complete_table_and_both_go_hash_algorithms(self):
        files = embedded_files(elf_fixture())
        self.assertEqual(set(files), {'index.html', 'static/js/test.1234567890.js'})
        self.assertEqual(files['index.html'], b'<html>fixture</html>')

    def test_corruption_or_truncated_table_cannot_publish_partial_archive(self):
        binary = elf_fixture()
        corrupt = bytearray(binary)
        corrupt[binary.index(b'console.log')] ^= 1
        wrong_count = bytearray(binary)
        struct.pack_into('<Q', wrong_count, 496, 5)
        for value in (bytes(corrupt), bytes(wrong_count), b'not ELF', binary[:500]):
            with self.assertRaises((AssetError, struct.error)):
                embedded_files(value)

    def test_only_versioned_asset_paths_are_public(self):
        for path in ('static/js/test.1234567890.js', 'static/fonts/PublicSans-1234abcd.woff2'):
            self.assertTrue(immutable_path(path))
        for path in ('index.html', 'static/js/../secret.12345678.js', 'static/js/plain.js',
                     'api/file.12345678.js', 'assets/.env', '/static/file.12345678.js'):
            self.assertFalse(immutable_path(path))

    def test_publish_is_idempotent_and_rejects_conflicting_hash_path(self):
        binary = elf_fixture()
        def copy(args, **_):
            Path(args[-1]).write_bytes(binary)
            return subprocess.CompletedProcess(args, 0)
        with tempfile.TemporaryDirectory() as folder, patch('static_assets.subprocess.run', side_effect=copy):
            directory = Path(folder) / 'public'
            first = retain_container('fixture', 'sha256:' + '1' * 64, directory)
            self.assertEqual(first, retain_container('fixture', first['image'], directory))
            self.assertFalse((directory / 'index.html').exists())
            self.assertEqual(len(first['assets']), 1)
            target = directory / next(iter(first['assets']))
            original = target.read_bytes()
            binary = elf_fixture(b'changed content with the same hash path')
            with self.assertRaises(AssetError):
                retain_container('fixture', 'sha256:' + '2' * 64, directory)
            self.assertEqual(target.read_bytes(), original)


if __name__ == '__main__':
    unittest.main()
