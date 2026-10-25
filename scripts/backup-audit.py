"""Read-only MySQL backup capability audit. Never prints credentials or row data."""
import json
import subprocess


def command(*args):
    result = subprocess.run(args, capture_output=True, check=True)
    return result.stdout.decode().strip()


def audit(container):
    info = json.loads(command('docker', 'inspect', container))[0]
    env = dict(x.split('=', 1) for x in info['Config']['Env'] if '=' in x)
    password = env.get('MYSQL_ROOT_PASSWORD')
    if not password:
        raise RuntimeError('No configured audit credential; supply a scoped backup account first')
    def query(statement):
        return command('docker', 'exec', '-e', 'MYSQL_PWD=' + password, container,
                       'mysql', '-uroot', '--batch', '--raw', '--skip-column-names', '-e', statement)
    settings = dict(line.split('\t', 1) for line in query(
        "SHOW GLOBAL VARIABLES WHERE Variable_name IN ('version','log_bin','binlog_format',"
        "'binlog_expire_logs_seconds','gtid_mode','enforce_gtid_consistency','server_uuid',"
        "'sync_binlog','innodb_flush_log_at_trx_commit')").splitlines())
    return {'settings': settings, 'binary_logs': query('SHOW BINARY LOGS').splitlines(),
            'position': query('SHOW BINARY LOG STATUS').splitlines(),
            'non_transactional_tables': query("SELECT COUNT(*) FROM information_schema.tables WHERE "
                "TABLE_SCHEMA NOT IN ('mysql','sys','performance_schema','information_schema') "
                "AND TABLE_TYPE='BASE TABLE' AND ENGINE<>'InnoDB'"),
            'archive_verified': False, 'recovery_drill_verified': False,
            'may_replace_stopped_dump': False}


if __name__ == '__main__':
    import sys
    try:
        print(json.dumps(audit(sys.argv[1]), ensure_ascii=False, indent=2))
    except Exception:
        sys.exit('Backup audit failed; credentials and subprocess output suppressed')
