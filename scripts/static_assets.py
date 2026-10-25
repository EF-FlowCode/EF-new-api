"""Retain immutable frontend assets independently of the two request slots.

Legacy release images contain only a Go ELF executable. Read its embed.FS table
without executing it; verify the slice header and every embedded SHA256 prefix.
Unsupported layouts fail before cutover. No application data is read or served.
"""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import struct
import subprocess
import tempfile


class AssetError(RuntimeError):
    pass


def embedded_files(binary):
    if binary[:6] != b'\x7fELF\x02\x01':
        raise AssetError('Static archive requires a little-endian ELF64 image')
    phoff = struct.unpack_from('<Q', binary, 32)[0]
    size, count = struct.unpack_from('<HH', binary, 54)
    segments = []
    for n in range(count):
        kind, flags, offset, address, _, length, _, _ = struct.unpack_from('<IIQQQQQQ', binary, phoff + n * size)
        if kind == 1 and not flags & 1:
            segments.append((offset, address, length))

    def offset_of(address, length):
        for offset, base, size in segments:
            if base <= address and address + length <= base + size:
                return offset + address - base
        raise AssetError('Embedded pointer outside file-backed data')

    def address_of(offset):
        for start, base, size in segments:
            if start <= offset < start + size:
                return base + offset - start
        raise AssetError('Embedded table outside data')

    def entry(offset):
        name_ptr, name_len, data_ptr, data_len = struct.unpack_from('<QQQQ', binary, offset)
        if not 3 <= name_len <= 512 or data_len > 64 * 1024 * 1024:
            raise AssetError('Invalid embedded entry size')
        pos = offset_of(name_ptr, name_len)
        raw_name = binary[pos:pos + name_len].decode('utf-8')
        name = raw_name.removesuffix('/')
        if (name != 'web' and not name.startswith('web/')) or not re.fullmatch(r'[A-Za-z0-9_./@+-]+', name):
            raise AssetError('Unexpected embedded path')
        if any(p in ('', '.', '..') for p in name.split('/')):
            raise AssetError('Unsafe embedded path')
        checksum = binary[offset + 32:offset + 48]
        if raw_name.endswith('/') and checksum == bytes(16) and data_len == 0 and data_ptr == 0:
            return name, None
        pos = offset_of(data_ptr, data_len)
        data = binary[pos:pos + data_len]
        # Go 1.26 cmd/internal/hash: small files flip SHA256 byte 0;
        # streamed files hash a leading 0x01 byte (staticdata/data.go).
        expected = bytearray(hashlib.sha256(data if data_len <= 1024 else b'\x01' + data).digest()[:16])
        if data_len <= 1024:
            expected[0] ^= 0xff
        if expected != checksum:
            raise AssetError('Embedded asset checksum mismatch')
        return name, data

    tables = {}
    needle = b'web/dist/index.html'
    for match in re.finditer(re.escape(needle), binary):
        try:
            pointer = struct.pack('<QQ', address_of(match.start()), len(needle))
        except AssetError:
            continue
        for ref in re.finditer(re.escape(pointer), binary):
            start = ref.start()
            try:
                entry(start)
                while start >= 48:
                    try:
                        entry(start - 48)
                    except (AssetError, UnicodeError, struct.error):
                        break
                    start -= 48
                entries = []
                end = start
                while True:
                    try:
                        entries.append(entry(end))
                    except (AssetError, UnicodeError, struct.error):
                        break
                    end += 48
                header = struct.pack('<QQQ', address_of(start), len(entries), len(entries))
                if binary[start - 24:start] != header or entries[0] != ('web', None) or len(entries) < 3:
                    continue
                files = dict(entries)
                if len(files) != len(entries) or files.get('web/dist/index.html') is None:
                    continue
                # A directory/file table is complete only when every parent exists.
                if any(str(PurePosixPath(n).parent) not in files for n in files if n != 'web'):
                    continue
                tables[start] = {n.removeprefix('web/dist/'): data for n, data in entries
                                 if n.startswith('web/dist/') and data is not None}
            except (AssetError, UnicodeError, struct.error):
                continue
    if len(tables) != 1:
        raise AssetError('Cannot prove a unique, complete frontend embed table')
    return next(iter(tables.values()))


def immutable_path(name):
    return bool(re.fullmatch(r'(?:static|assets)/(?:[A-Za-z0-9_@+-]+/)*[A-Za-z0-9_@+.-]*[.-][a-f0-9]{8,64}(?:\.[A-Za-z0-9]+)+', name))


def retain_container(container, image, directory):
    if not re.fullmatch(r'sha256:[a-f0-9]{64}', image):
        raise AssetError('Archive requires an immutable image identity')
    directory = Path(directory)
    directory.mkdir(parents=True, exist_ok=True)
    directory.chmod(0o755)
    # Always re-read immutable image content; never trust a stale manifest alone.
    with tempfile.TemporaryDirectory(prefix='newapi-static-') as temporary:
        executable = Path(temporary) / 'new-api'
        result = subprocess.run(['docker', 'cp', container + ':/new-api', str(executable)], capture_output=True)
        if result.returncode:
            raise AssetError('Unable to read frontend from the request container')
        files = embedded_files(executable.read_bytes())
    assets = {name: data for name, data in files.items() if immutable_path(name)}
    unsupported = [name for name in files if name.startswith(('static/', 'assets/')) and name not in assets]
    if unsupported or not any(n.startswith('static/js/') for n in assets):
        raise AssetError('Frontend contains unsupported non-versioned assets')
    # Preflight all collisions before publishing. Hash names must never be replaced.
    for name, data in assets.items():
        target = directory / name
        if target.is_symlink() or any(p.is_symlink() for p in target.parents):
            raise AssetError('Static archive must not contain symlinks')
        if target.exists() and target.read_bytes() != data:
            raise AssetError('Static asset path collision: ' + name)
    for name, data in assets.items():
        target = directory / name
        target.parent.mkdir(parents=True, exist_ok=True)
        parent = target.parent
        while parent != directory:
            parent.chmod(0o755)
            parent = parent.parent
        if not target.exists():
            temporary = target.with_suffix(target.suffix + '.tmp')
            temporary.write_bytes(data)
            temporary.chmod(0o644)
            os.replace(temporary, target)
    manifest = {'image': image, 'embedded_files': len(files), 'assets': {
        name: hashlib.sha256(data).hexdigest() for name, data in sorted(assets.items())}}
    # Manifests live outside the public resource subtree and cannot be requested.
    archive = directory.parent / 'asset-manifests'
    archive.mkdir(exist_ok=True)
    target = archive / (image[7:] + '.json')
    temporary = target.with_suffix('.tmp')
    temporary.write_text(json.dumps(manifest, sort_keys=True), encoding='utf-8')
    os.replace(temporary, target)
    return manifest
