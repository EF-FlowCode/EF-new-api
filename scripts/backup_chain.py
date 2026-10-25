"""Verify immutable snapshot/binlog recovery chains before considering fast backup.

The current release policy remains stopped_full. Merely finding binlogs or a
matching checksum never grants permission to replace a stopped backup.
"""
import json
from pathlib import Path
import re
import subprocess
import time

from release_core import ReleaseError, contained, digest, file_hash


def position(value):
    if not isinstance(value, dict) or not re.fullmatch(r'[A-Za-z0-9_-]+\.[0-9]{6,}', value.get('file', '')):
        raise ReleaseError('Invalid binlog position')
    if type(value.get('position')) is not int or value['position'] < 4:
        raise ReleaseError('Invalid binlog byte offset')
    prefix, number = value['file'].rsplit('.', 1)
    return prefix, int(number), value['position']


def verify_archive(directory, manifest):
    if manifest.get('schema') != 1 or manifest.get('binlog_format') != 'ROW':
        raise ReleaseError('Unknown backup chain format')
    if not manifest.get('server_uuid') or not manifest.get('databases') or not all(
            re.fullmatch(r'[A-Za-z0-9_]+', name) for name in manifest['databases']):
        raise ReleaseError('Backup source identity/scope is missing')
    start, end = position(manifest['snapshot_position']), position(manifest['cutoff_position'])
    if start[0] != end[0] or start[1:] > end[1:]:
        raise ReleaseError('Recovery positions are out of order or from different binlog series')
    snapshot = manifest['snapshot']
    path = contained(directory, snapshot['file'])
    if path.stat().st_size != snapshot['bytes'] or file_hash(path) != snapshot['sha256']:
        raise ReleaseError('Snapshot is missing or changed')
    logs = manifest.get('binlogs', [])
    expected = list(range(start[1], end[1] + 1))
    actual = []
    for entry in logs:
        prefix, number, offset = position({'file': entry['file'], 'position': 4})
        if prefix != start[0]:
            raise ReleaseError('Mixed binlog series')
        actual.append(number)
        path = contained(directory, entry['file'])
        if path.stat().st_size != entry['bytes'] or file_hash(path) != entry['sha256']:
            raise ReleaseError('Binlog segment is missing or changed')
        with path.open('rb') as stream:
            if stream.read(4) != b'\xfebin':
                raise ReleaseError('Not a raw binlog')
        if number == end[1] and entry['bytes'] < end[2]:
            raise ReleaseError('Archive has not reached the cutoff position')
        if number == start[1] and entry['bytes'] < start[2]:
            raise ReleaseError('Snapshot position is beyond the first archive segment')
        if number != end[1] and entry.get('closed') is not True:
            raise ReleaseError('Intermediate binlog was not archived completely')
    if actual != expected:
        raise ReleaseError('Binlog gap, duplicate or wrong order')
    return {'result': 'passed', 'chain_sha256': digest(manifest), 'cutoff_position': manifest['cutoff_position']}


def verify_chain(directory, manifest, restore_proof=None, now=None):
    now = time.time() if now is None else now
    result = verify_archive(directory, manifest)
    if not restore_proof or restore_proof.get('result') != 'passed':
        raise ReleaseError('A real isolated restore proof is required')
    if restore_proof.get('chain_sha256') != digest(manifest) or restore_proof.get('server_uuid') != manifest['server_uuid']:
        raise ReleaseError('Restore proof belongs to another backup chain')
    if restore_proof.get('cutoff_position') != manifest['cutoff_position'] or not restore_proof.get('isolated_resources_cleaned'):
        raise ReleaseError('Restore boundary or cleanup was not verified')
    if not restore_proof.get('schema_and_data_match') or not restore_proof.get('application_restart_passed'):
        raise ReleaseError('Restored schema/data and application restart must be verified')
    if not 0 <= now - restore_proof.get('verified_at', 0) <= 86400:
        raise ReleaseError('Restore proof is stale')
    return result


def replay_command(manifest, mount='/chain'):
    # Filtering ROW events requires an explicit database. Multi-database replay
    # needs a separately reviewed cross-database transaction contract.
    if len(manifest['databases']) != 1:
        raise ReleaseError('Isolated restore currently requires one explicit database')
    return ['mysqlbinlog', '--verify-binlog-checksum', '--database=' + manifest['databases'][0],
            '--start-position=' + str(manifest['snapshot_position']['position']),
            '--stop-position=' + str(manifest['cutoff_position']['position']),
            *[mount + '/' + entry['file'] for entry in manifest['binlogs']]]


def archive_command(client, directory, first_file):
    """Build the continuous RAW archive command; no credentials on command line.

    client is a private MySQL defaults-extra-file supplied by the operator.
    The process must be supervised; interruption stops advancement of the
    verified position and must never silently select the newest remote log.
    """
    position({'file': first_file, 'position': 4})
    return ['mysqlbinlog', '--defaults-extra-file=' + str(Path(client).resolve()),
            '--read-from-remote-server', '--raw', '--stop-never', '--verify-binlog-checksum',
            '--result-file=' + str(Path(directory).resolve()) + '/', first_file]


def snapshot_command(client, databases):
    if not databases or not all(re.fullmatch(r'[A-Za-z0-9_]+', x) for x in databases):
        raise ReleaseError('Explicit database scope is required')
    return ['mysqldump', '--defaults-extra-file=' + str(Path(client).resolve()),
            '--single-transaction', '--source-data=2', '--set-gtid-purged=OFF', '--quick',
            '--routines', '--events', '--triggers', '--hex-blob', '--databases', *databases]


if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('manifest', type=Path)
    parser.add_argument('--restore-proof', type=Path, required=True)
    args = parser.parse_args()
    print(json.dumps(verify_chain(args.manifest.parent, json.loads(args.manifest.read_text()),
                                 json.loads(args.restore_proof.read_text())), indent=2))
