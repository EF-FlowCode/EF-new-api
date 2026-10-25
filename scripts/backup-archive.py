"""Consistent online snapshot and supervised continuous raw binlog archive.

Run on a backup host with MySQL 8 client binaries and a private defaults file.
This command does not enable the fast release path or change MySQL settings.
"""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

from backup_chain import archive_command, snapshot_command, position, verify_archive
from release import save
from release_core import ReleaseError, file_hash


def query(client, statement):
    result = subprocess.run(['mysql', '--defaults-extra-file=' + str(client), '--batch',
                             '--raw', '--skip-column-names', '-e', statement], capture_output=True, timeout=30)
    if result.returncode:
        raise ReleaseError('Backup source query failed; credentials suppressed')
    return result.stdout.decode().strip()


def snapshot(client, directory, databases):
    if (directory / 'snapshot.json').exists() or (directory / 'snapshot.sql').exists():
        raise ReleaseError('Use a new snapshot directory; never overwrite a recovery baseline')
    source = query(client, 'SELECT @@server_uuid,@@binlog_format,@@log_bin').split('\t')
    if len(source) != 3 or source[1:] != ['ROW', '1']:
        raise ReleaseError('ROW binlog is required')
    db_hex = ','.join('CONVERT(0x' + name.encode().hex() + ' USING utf8mb4)' for name in databases)
    count = query(client, "SELECT COUNT(*) FROM information_schema.tables WHERE TABLE_SCHEMA IN (" + db_hex +
                  ") AND TABLE_TYPE='BASE TABLE' AND ENGINE<>'InnoDB'")
    if count != '0':
        raise ReleaseError('Online transaction snapshot requires exclusively InnoDB tables')
    path = directory / 'snapshot.sql.partial'
    with path.open('wb') as output, (directory / 'snapshot.error.log').open('ab') as error:
        result = subprocess.run(snapshot_command(client, databases), stdout=output, stderr=error)
        output.flush()
        os.fsync(output.fileno())
    if result.returncode:
        raise ReleaseError('Consistent snapshot failed; partial evidence retained')
    with path.open('rb') as stream:
        header = stream.read(128 * 1024).decode('utf-8', errors='replace')
        stream.seek(max(0, path.stat().st_size - 1024))
        if b'Dump completed on' not in stream.read():
            raise ReleaseError('Snapshot has no completion marker')
    match = re.search(r"SOURCE_LOG_FILE='([^']+)', SOURCE_LOG_POS=([0-9]+)", header)
    if not match:
        raise ReleaseError('Snapshot has no exact binary log position')
    final = directory / 'snapshot.sql'
    path.replace(final)
    save(directory / 'snapshot.json', {'schema': 1, 'server_uuid': source[0], 'binlog_format': 'ROW',
         'databases': databases, 'snapshot_position': {'file': match[1], 'position': int(match[2])},
         'snapshot': {'file': final.name, 'bytes': final.stat().st_size, 'sha256': file_hash(final)},
         'created_at': time.time()})


def stream(client, directory):
    meta = json.loads((directory / 'snapshot.json').read_text())
    if query(client, 'SELECT @@server_uuid') != meta['server_uuid']:
        raise ReleaseError('Database instance changed; a new snapshot is required')
    first = meta['snapshot_position']['file']
    # A generation directory is never appended to after an unclean stop. Restart
    # reads again from the exact snapshot boundary, not the latest remote file.
    generation = directory / ('archive-' + str(time.time_ns()))
    generation.mkdir(mode=0o700)
    save(directory / 'archive-state.json', {'result': 'streaming', 'generation': generation.name, 'first_file': first})
    with (generation / 'error.log').open('ab') as error:
        result = subprocess.run(archive_command(client, generation, first), stdout=subprocess.DEVNULL, stderr=error)
    save(directory / 'archive-state.json', {'result': 'stopped', 'generation': generation.name,
        'first_file': first, 'exit_code': result.returncode, 'at': time.time()})
    raise ReleaseError('Continuous archive stopped; verify continuity before any release')


def seal(client, directory, cutoff):
    """Freeze an already archived prefix, without touching the running source."""
    meta = json.loads((directory / 'snapshot.json').read_text())
    if query(client, 'SELECT @@server_uuid') != meta['server_uuid']:
        raise ReleaseError('Source UUID changed')
    first, last = position(meta['snapshot_position']), position(cutoff)
    if first[0] != last[0] or first[1:] > last[1:]:
        raise ReleaseError('Invalid cutoff')
    generation = json.loads((directory / 'archive-state.json').read_text())['generation']
    if not re.fullmatch(r'archive-[0-9]+', generation):
        raise ReleaseError('Invalid archive generation')
    remote = {row.split('\t')[0]: int(row.split('\t')[1])
              for row in query(client, 'SHOW BINARY LOGS').splitlines()}
    current = query(client, 'SHOW BINARY LOG STATUS').split('\t')
    if remote.get(cutoff['file'], 0) < cutoff['position']:
        raise ReleaseError('Source no longer confirms the requested cutoff')
    output = directory / ('chain-' + str(time.time_ns()))
    output.mkdir(mode=0o700)
    # An immutable sealed chain owns its snapshot, independent of collector life.
    os.link(directory / meta['snapshot']['file'], output / meta['snapshot']['file'])
    entries = []
    for number in range(first[1], last[1] + 1):
        name = first[0] + '.' + str(number).zfill(6)
        source = directory / generation / name
        size = last[2] if number == last[1] else remote.get(name)
        if not size or source.stat().st_size < size or (number != last[1] and name == current[0]):
            raise ReleaseError('Archive is incomplete or intermediate segment is still open')
        target = output / name
        with source.open('rb') as origin, target.open('wb') as dest:
            remaining = size
            while remaining:
                chunk = origin.read(min(1024 * 1024, remaining))
                if not chunk:
                    raise ReleaseError('Archive truncated while sealing')
                dest.write(chunk)
                remaining -= len(chunk)
            dest.flush(); os.fsync(dest.fileno())
        entries.append({'file': name, 'bytes': size, 'sha256': file_hash(target), 'closed': number != last[1]})
    manifest = dict(meta, cutoff_position=cutoff, binlogs=entries)
    verify_archive(output, manifest)
    save(output / 'manifest.json', manifest)
    print(str(output))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['snapshot', 'stream', 'seal'])
    parser.add_argument('--client', type=Path, required=True)
    parser.add_argument('--directory', type=Path, required=True)
    parser.add_argument('--database', action='append', default=[])
    parser.add_argument('--cutoff-file')
    parser.add_argument('--cutoff-position', type=int)
    args = parser.parse_args()
    os.umask(0o077)
    args.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
    client = args.client.resolve(strict=True)
    if os.name != 'nt' and client.stat().st_mode & 0o077:
        raise ReleaseError('MySQL defaults file must be private (0600)')
    # Supervisor restarts must not run two binlog writers into the same chain.
    import fcntl
    with (args.directory / 'archive.lock').open('w') as lock:
        if args.action != 'seal':
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.action == 'snapshot':
            snapshot(client, args.directory, args.database)
        elif args.action == 'stream':
            stream(client, args.directory)
        else:
            seal(client, args.directory, {'file': args.cutoff_file, 'position': args.cutoff_position})


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        sys.exit('Backup operation incomplete: ' + type(error).__name__)
