"""Real isolated snapshot/continuous-binlog/restore test using existing images.

The seed is an existing-local SQL fixture. Never connects to production.
"""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import time
import uuid

from release import run, save
from release_core import ReleaseError, file_hash
from backup_chain import archive_command, snapshot_command


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def main(seed, image, output):
    archive = module('collector', 'backup-archive.py')
    restorer = module('restorer', 'backup-restore.py')
    prefix = 'backup-drill-' + uuid.uuid4().hex[:12]
    output.mkdir(parents=True, exist_ok=False)
    mysql = prefix + '-mysql'
    password = uuid.uuid4().hex
    base = ['docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + password, mysql]
    collector = None
    collector_name = prefix + '-collector'
    report = {'result': 'running', 'production_writes': False}
    def sql(statement):
        return run(base + ['mysql', '-uroot', '--batch', '--raw', '--skip-column-names'], input_data=statement.encode())
    def client_args(args):
        # Transport only: the production mysqldump/mysqlbinlog flags are kept.
        return base + [args[0], '--defaults-extra-file=/tmp/client.cnf', *args[2:]]
    try:
        run(['docker', 'run', '-d', '--name', mysql, '--network', 'none',
             '-e', 'MYSQL_ROOT_PASSWORD=' + password, 'mysql:8.4.8'])
        deadline = time.monotonic() + 90
        while True:
            try:
                sql('SELECT 1'); break
            except ReleaseError:
                if time.monotonic() > deadline: raise
                time.sleep(1)
        sql('CREATE DATABASE drill')
        with seed.open('rb') as stream:
            result = subprocess.run(base + ['mysql', '-uroot', 'drill'], stdin=stream,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=120)
        if result.returncode:
            raise ReleaseError('Isolated seed import failed')
        defaults = output / 'client.cnf'
        defaults.write_text('[client]\nuser=root\npassword=' + password + '\nhost=127.0.0.1\n', encoding='utf-8')
        run(['docker', 'cp', str(defaults), mysql + ':/tmp/client.cnf'])
        run(['docker', 'exec', mysql, 'chmod', '600', '/tmp/client.cnf'])
        archive.query = lambda client, statement: sql(statement)
        archive.snapshot_command = lambda client, databases: client_args(snapshot_command(client, databases))
        chain = output / 'collector'
        chain.mkdir()
        archive.snapshot(defaults, chain, ['drill'])
        meta = json.loads((chain / 'snapshot.json').read_text())
        generation = chain / 'archive-1'
        generation.mkdir()
        args = archive_command(defaults, chain, meta['snapshot_position']['file'])
        args = ['--result-file=/raw/' if arg.startswith('--result-file=') else
                '--defaults-extra-file=/tmp/private.cnf' if arg.startswith('--defaults-extra-file=') else arg for arg in args]
        args = ['docker', 'run', '--rm', '--name', collector_name, '--network', 'container:' + mysql,
                '--mount', 'type=bind,source=' + str(defaults.resolve()) + ',target=/client.cnf,readonly',
                '--mount', 'type=bind,source=' + str(generation.resolve()) + ',target=/raw',
                'new-api-backup-client:8.4.8', 'sh', '-c',
                'cp /client.cnf /tmp/private.cnf && chmod 600 /tmp/private.cnf && exec "$@"', 'sh', *args]
        with (output / 'collector.log').open('wb') as log:
            collector = subprocess.Popen(args, stdout=log, stderr=log)
        sql('USE drill; CREATE TABLE drill.release_restore_probe(id INT PRIMARY KEY, value VARCHAR(50)); '
            "INSERT INTO drill.release_restore_probe VALUES(1,'before-rotation'); FLUSH BINARY LOGS")
        sql("UPDATE drill.release_restore_probe SET value='after-rotation'; FLUSH BINARY LOGS")
        sql("INSERT INTO drill.release_restore_probe VALUES(2,'at-cutoff')")
        status = sql('SHOW BINARY LOG STATUS').split('\t')
        cutoff = {'file': status[0], 'position': int(status[1])}
        original_dump = subprocess.run(base + restorer.DUMP_ARGS + ['drill'], capture_output=True, timeout=120)
        if original_dump.returncode:
            raise ReleaseError('Independent source hash failed')
        expected = hashlib.sha256(original_dump.stdout).hexdigest()
        save(output / 'cutoff.json', {'source_hash': expected, 'cutoff': cutoff})
        # Close the final file so the raw collector flushes it before export.
        sql('FLUSH BINARY LOGS')
        deadline = time.monotonic() + 30
        while True:
            try:
                length = (generation / cutoff['file']).stat().st_size
                if length >= cutoff['position']: break
            except (ReleaseError, FileNotFoundError):
                pass
            if time.monotonic() > deadline:
                raise ReleaseError('Continuous archive did not reach cutoff')
            time.sleep(0.5)
        save(chain / 'archive-state.json', {'generation': generation.name})
        archive.seal(defaults, chain, cutoff)
        sealed = next(chain.glob('chain-*'))
        proof = restorer.restore(sealed, image, expected, output / 'restore')
        report.update(result='passed', snapshot_plus_three_binlogs=True, source_hash=expected,
                      restore_proof=proof, application_image_id=proof['application_image_id'])
    finally:
        # Generated source container only. mysqlbinlog is stopped with its source.
        run(['docker', 'rm', '-f', '-v', mysql])
        if collector:
            subprocess.run(['docker', 'rm', '-f', collector_name], capture_output=True, timeout=15)
            collector.wait(timeout=15)
        report['source_resources_cleaned'] = True
        save(output / 'report.json', report)
    print('SNAPSHOT_BINLOG_RESTORE_PASSED')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--seed', type=Path, required=True)
    parser.add_argument('--image', required=True)
    parser.add_argument('--evidence', type=Path, required=True)
    args = parser.parse_args()
    main(args.seed, args.image, args.evidence)
