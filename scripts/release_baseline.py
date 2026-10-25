"""Immutable production-derived baselines and fresh, isolated upgrade clones."""
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import secrets
import shutil
import socket
import subprocess
import time
import urllib.request

from release import PRIVATE, read, run, save
from release_core import ReleaseError, contained, digest, file_hash

SCOPE = 'production_schema_pricing_sanitized_samples'


def select_validation_subnets(pool, occupied):
    network = ipaddress.IPv4Network(pool)
    if not network.is_private or not 16 <= network.prefixlen <= 27:
        raise ReleaseError('Validation subnet pool must be a private IPv4 /16 through /27')
    used = [ipaddress.ip_network(value) for value in occupied]
    available = []
    for candidate in network.subnets(new_prefix=28):
        if not any(other.version == 4 and candidate.overlaps(other) for other in used):
            available.append(str(candidate))
            if len(available) == 2:
                return available
    raise ReleaseError('Validation subnet pool exhausted; existing networks were not removed')


def candidate_roles(mode, candidate, old_worker, worker_handoff=False):
    return {'web': candidate, 'worker': candidate if mode == 'maintenance' or worker_handoff else old_worker}


def validation_networks(networks):
    """Apply the explicitly configured isolated pool to rehearsal networks too."""
    pool = os.environ.get('NEW_API_VALIDATION_SUBNET_POOL')
    if not pool:
        return networks
    ids = run(['docker', 'network', 'ls', '-q']).splitlines()
    inspected = json.loads(run(['docker', 'network', 'inspect', *ids])) if ids else []
    occupied = [item['Subnet'] for net in inspected for item in (net.get('IPAM', {}).get('Config') or []) if item.get('Subnet')]
    subnets = iter(select_validation_subnets(pool, occupied))
    for config in networks.values():
        if not config.get('external'):
            config['ipam'] = {'config': [{'subnet': next(subnets)}]}
    return networks


def rehearsal_user_tier(options, routing_group):
    """Choose an existing tier; never grant a group or change production options.

    The old application's groups API remains the authority after login. This
    mirrors its configured add/remove rules only to select a synthetic identity.
    """
    ratios = json.loads(options.get('GroupRatio', '{}'))
    if routing_group not in ratios:
        raise ReleaseError('Rehearsal routing group has no production ratio: ' + routing_group)
    usable = json.loads(options.get('UserUsableGroups', '{"default":"default","vip":"vip"}'))
    special = json.loads(options.get('group_ratio_setting.group_special_usable_group', '{}'))
    for tier in ['default', *sorted(set(special) - {'default'})]:
        groups = dict(usable)
        rules = special.get(tier, {})
        # Conflicting aliases depend on Go map iteration; do not choose that tier.
        targets = [key[2:] if key.startswith(('-:', '+:')) else key for key in rules]
        if len(set(targets)) != len(targets):
            continue
        for group, description in rules.items():
            if group.startswith('-:'):
                groups.pop(group[2:], None)
            else:
                groups[group[2:] if group.startswith('+:') else group] = description
        if routing_group in groups:
            return tier
    raise ReleaseError('No configured customer tier authorizes rehearsal group: ' + routing_group)


def value_sql(value):
    if value is None:
        return 'NULL'
    if isinstance(value, (int, float)):
        return str(value)
    if str(value) == '':
        return "''"
    return 'CONVERT(0x' + str(value).encode('utf-8').hex() + ' USING utf8mb4)'


def insert_sql(table, values):
    if not all(re.fullmatch(r'[a-z_]+', name) for name in [table, *values]):
        raise ReleaseError('Invalid fixture column')
    return 'INSERT INTO `' + table + '` (' + ','.join('`' + k + '`' for k in values) + ') VALUES (' + \
        ','.join(value_sql(v) for v in values.values()) + ');'


def resolve_task_sources(snapshot, revision):
    """Resolve only reviewed Git blobs by exact production hashes; no remote JS export."""
    from release import ROOT
    task = snapshot.get('task_pricing', {})
    if task.get('schema') != 1 or len(task.get('control', [])) != 1:
        raise ReleaseError('Complete task pricing topology/control snapshot required')
    for row in task['plugins'] + task['sources']:
        if row['actual_source_hash'] != row['source_hash']:
            raise ReleaseError('Production plugin source hash mismatch: ' + row['key'])
    resolved = {}
    for row in task['bindings'] + task['plugins'] + task['sources']:
        checksum, key = row['source_hash'], row['key']
        if checksum in resolved:
            if (resolved[checksum]['key'], resolved[checksum]['version']) != (key, row['version']):
                raise ReleaseError('Conflicting fixed source identity')
            continue
        if not re.fullmatch(r'[a-z0-9-]+', key) or not re.fullmatch(r'[a-f0-9]{64}', checksum):
            raise ReleaseError('Invalid fixed plugin source identity')
        blob = subprocess.run(['git', 'show', revision + ':plugins/tasks/' + key + '/plugin.js'],
                              cwd=ROOT, capture_output=True)
        if blob.returncode or hashlib.sha256(blob.stdout).hexdigest() != checksum:
            raise ReleaseError('Fixed plugin source is not the deployed reviewed Git blob: ' + key +
                               '; explicit source review required; no substituted source')
        resolved[checksum] = {'source_hash': checksum, 'key': key, 'version': row['version'],
                              'api_version': row.get('api_version', 1), 'source': blob.stdout.decode('utf-8')}
    return resolved


def seed_sql(snapshot, password_hash, task_sources=None):
    statements = [insert_sql('setups', dict(initialization_record(snapshot), id=1))]
    for key, value in snapshot['pricing'].items():
        statements.append(insert_sql('options', {'key': key, 'value': value}))
    statements.append(insert_sql('users', {'id': 1, 'username': 'baseline_root', 'password': password_hash,
        'display_name': 'Baseline Root', 'role': 100, 'status': 1, 'quota': 10000000,
        'group': 'default', 'aff_code': 'baseline-root', 'setting': '{}'}))
    samples = snapshot['samples']
    # IDs, credentials, provider and customer identities are always local-only.
    for i, row in enumerate(samples['users'], 100):
        statements.append(insert_sql('users', dict(row, id=i, username='sample_' + str(i),
            password=password_hash, display_name='Sanitized Sample', role=1, group='default',
            aff_code='sample-' + str(i), setting='{}')))
    for i, row in enumerate(samples['tokens'], 100):
        statements.append(insert_sql('tokens', dict(row, id=i, user_id=1, key=secrets.token_hex(24),
            name='sample-token-' + str(i), group='default', expired_time=-1)))
    task = snapshot['task_pricing']
    for row in task['channels']:
        statements.append(insert_sql('channels', dict(row, key='local-mock-only',
            name='baseline-channel-' + str(row['id']), base_url='http://upstream:8080')))
    statements.append(insert_sql('task_plugin_controls', dict(task['control'][0], id=1)))
    for row in task['bindings']:
        statements.append(insert_sql('task_plugin_production_bindings', row))
    for i, row in enumerate(task['plugins'], 1):
        fields = {k: v for k, v in row.items() if k != 'actual_source_hash'}
        fields.update(id=i, source=task_sources[row['source_hash']]['source'], created_at=0)
        statements.append(insert_sql('task_plugins', fields))
    for row in (task_sources or {}).values():
        statements.append(insert_sql('task_plugin_source_snapshots', row))
    sample_channel = task['channels'][0]['id'] if task['channels'] else 0
    for i, row in enumerate(samples['logs'], 100):
        statements.append(insert_sql('logs', dict(row, id=i, user_id=1, username='baseline_root',
            channel_id=sample_channel, created_at=1700000000 + i, model_name='gpt-5-codex', other='{}')))
    for i, row in enumerate(samples['tasks'], 100):
        # Terminal samples cannot resume a production task or poll a real provider.
        statements.append(insert_sql('tasks', dict(row, id=i, user_id=1, channel_id=sample_channel,
            task_id='local-sample-' + str(i), group='default', action='sample', progress='100%',
            submit_time=1700000000, finish_time=1700000001, properties='{}', private_data='{}', data='{}')))
    return '\n'.join(statements).encode('utf-8')


def initialization_record(snapshot):
    record = snapshot.get('setup')
    if (not isinstance(record, dict) or set(record) != {'version', 'initialized_at'}
            or not isinstance(record['version'], str) or not 0 < len(record['version']) <= 50
            or type(record['initialized_at']) is not int or record['initialized_at'] <= 0):
        raise ReleaseError('Baseline requires valid production initialization metadata; capture a new snapshot')
    return record


def require_initialized(response):
    if response.get('success') is not True or response.get('data', {}).get('status') is not True:
        raise ReleaseError('Baseline/clone initialization is incomplete; /api/setup must report status=true')


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


class Stack:
    def __init__(self, directory, spec):
        self.directory = Path(directory)
        self.spec = spec
        self.path = self.directory / 'stack.json'

    @classmethod
    def create(cls, directory, prefix, web, worker, contract, owner, subnet_pool=None):
        if not re.fullmatch(r'newapi-(baseline|upgrade)-[a-f0-9]{12}', prefix):
            raise ReleaseError('Invalid isolated stack identity')
        directory = Path(directory)
        directory.mkdir(parents=True, exist_ok=True)
        password = secrets.token_hex(24)
        port = free_port()
        spec = {'prefix': prefix, 'web': prefix + '-web', 'worker': prefix + '-worker',
            'mysql': prefix + '-mysql', 'redis': prefix + '-redis', 'origin': 'http://127.0.0.1:' + str(port),
            'password': password, 'contract': contract, 'owner': owner}
        env = {'SQL_DSN': f'root:{password}@tcp(mysql:3306)/new_api?charset=utf8mb4&parseTime=True&loc=Local',
            'REDIS_CONN_STRING': 'redis://redis:6379', 'SESSION_SECRET': secrets.token_hex(32),
            'BATCH_UPDATE_ENABLED': 'false', 'CHANNEL_UPSTREAM_MODEL_UPDATE_TASK_ENABLED': 'false',
            'SYNC_FREQUENCY': '1', 'SQL_MAX_OPEN_CONNS': '10', 'TZ': 'Asia/Shanghai'}
        labels = {'cc.e-flowcode.validation-owner': owner, 'cc.e-flowcode.validation-stack': prefix}
        services = {
            'mysql': {'image': 'mysql:8.4.8', 'container_name': spec['mysql'], 'networks': ['runtime'],
                'environment': {'MYSQL_ROOT_PASSWORD': password, 'MYSQL_DATABASE': 'new_api'},
                'volumes': ['mysql-data:/var/lib/mysql'], 'labels': labels},
            'redis': {'image': 'redis:7-alpine', 'container_name': spec['redis'], 'networks': ['runtime'], 'labels': labels},
        }
        for role, image in [('web', web), ('worker', worker), ('migrate', worker)]:
            services[role] = {'image': image, 'container_name': prefix + '-' + role, 'restart': 'no',
                'stop_grace_period': '130s', 'networks': ['runtime'], 'volumes': ['app-data:/data'],
                'labels': labels, 'command': ['--log-dir', '/data/logs'],
                'environment': dict(env, DEPLOYMENT_ROLE=role, NODE_TYPE='slave' if role == 'web' else 'master',
                                    NODE_NAME=prefix + '-' + role)}
        nginx = directory / 'gateway.conf'
        nginx.write_text('server { listen 3000; resolver 127.0.0.11 valid=1s ipv6=off; '
            'location / { set $backend web:3000; proxy_pass http://$backend; proxy_set_header Host $host; } }\n')
        services['gateway'] = {'image': 'nginx:1.28-alpine', 'container_name': prefix + '-gateway',
            'networks': ['runtime', 'front'], 'ports': ['127.0.0.1:' + str(port) + ':3000'], 'labels': labels,
            'volumes': [{'type': 'bind', 'source': str(nginx.resolve()), 'target': '/etc/nginx/conf.d/default.conf', 'read_only': True}]}
        config = {'name': prefix, 'services': services, 'networks': {'runtime': {'internal': True}, 'front': {}},
            'volumes': {'mysql-data': {}, 'app-data': {}}}
        if subnet_pool:
            ids = run(['docker', 'network', 'ls', '-q']).splitlines()
            networks = json.loads(run(['docker', 'network', 'inspect', *ids])) if ids else []
            occupied = [item['Subnet'] for net in networks for item in (net.get('IPAM', {}).get('Config') or [])
                        if item.get('Subnet')]
            for name, subnet in zip(('runtime', 'front'), select_validation_subnets(subnet_pool, occupied)):
                config['networks'][name]['ipam'] = {'config': [{'subnet': subnet}]}
        save(directory / 'stack.json', config)
        save(directory / 'identity.json', spec)
        return cls(directory, spec)

    def compose(self, *args, timeout=180):
        return run(['docker', 'compose', '-f', str(self.path), *args], timeout=timeout)

    def inspect(self, name):
        info = json.loads(run(['docker', 'inspect', name]))[0]
        if info['Config'].get('Labels', {}).get('cc.e-flowcode.validation-owner') != self.spec['owner']:
            raise ReleaseError('Unowned validation container')
        return info

    def sql(self, statement):
        return run(['docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + self.spec['password'], self.spec['mysql'],
            'mysql', '-uroot', '--default-character-set=utf8mb4', '--raw', '--batch', '--skip-column-names', 'new_api'],
            input_data=statement.encode() if isinstance(statement, str) else statement, timeout=120)

    def start_database(self):
        self.compose('up', '-d', 'mysql', 'redis')
        deadline = time.monotonic() + 120
        while True:
            probe = subprocess.run(['docker', 'exec', '-e', 'MYSQL_PWD=' + self.spec['password'], self.spec['mysql'],
                'mysql', '-uroot', '-h127.0.0.1', '-e', 'SELECT 1'], capture_output=True)
            if probe.returncode == 0:
                return
            if time.monotonic() >= deadline:
                raise ReleaseError('Baseline/clone MySQL did not become ready')
            time.sleep(1)

    def ready(self, expected_contract=None):
        expected_contract = expected_contract or self.spec['contract']
        deadline = time.monotonic() + 90
        while True:
            try:
                for role in ('web', 'worker'):
                    data = json.loads(run(['docker', 'exec', self.spec[role], 'wget', '-q', '-T', '3', '-O', '-',
                                           'http://127.0.0.1:3001/ready']))
                    if (not data.get('ready') or data.get('contract') != expected_contract
                            or data.get('role') != role or data.get('instance') != self.spec[role]):
                        raise ValueError('Readiness/contract mismatch')
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                with opener.open(self.spec['origin'] + '/api/status', timeout=4) as response:
                    status = json.load(response)
                if status.get('success') is not True:
                    raise ValueError('Business API not ready')
                with opener.open(self.spec['origin'] + '/api/setup', timeout=4) as response:
                    setup = json.load(response)
            except (ReleaseError, OSError, ValueError):
                if time.monotonic() >= deadline:
                    raise ReleaseError('Baseline/clone readiness failed; containers and logs retained') from None
                time.sleep(1)
            else:
                require_initialized(setup)
                return status['data']['version']

    def dump(self, path):
        with Path(path).open('xb') as output:
            result = subprocess.run(['docker', 'exec', '-e', 'MYSQL_PWD=' + self.spec['password'], self.spec['mysql'],
                'mysqldump', '-uroot', '--single-transaction', '--quick', '--routines', '--events', '--triggers',
                '--hex-blob', '--set-gtid-purged=OFF', 'new_api'], stdout=output, stderr=subprocess.PIPE, timeout=180)
        if result.returncode or b'Dump completed on' not in Path(path).read_bytes()[-1024:]:
            raise ReleaseError('Baseline/clone database backup incomplete')

    def data_archive(self, path):
        with Path(path).open('xb') as output:
            result = subprocess.run(['docker', 'run', '--rm', '--network', 'none', '--volumes-from', self.spec['web'] + ':ro',
                '--entrypoint', 'tar', self.inspect(self.spec['web'])['Image'], '-czf', '-', '-C', '/data', '.'],
                stdout=output, stderr=subprocess.PIPE, timeout=120)
        if result.returncode:
            raise ReleaseError('Baseline/clone data archive failed')

    def archive_logs(self, directory):
        from release_local import archive_container_logs
        return {role: archive_container_logs(Path(directory), self.spec[role]) for role in ('web', 'worker')}

    def fingerprint(self):
        tables = self.sql('SHOW TABLES').splitlines()
        result = {}
        for table in tables:
            if not re.fullmatch(r'[a-zA-Z0-9_]+', table):
                raise ReleaseError('Unsupported baseline table name')
            rows = self.sql('SELECT * FROM `' + table + '`').splitlines()
            result[table] = {'rows': len(rows), 'sha256': digest(sorted(rows))}
        return result

    def schema(self):
        """Normalized CREATE TABLE text of every table; AUTO_INCREMENT counters excluded."""
        result = {}
        for table in self.sql('SHOW TABLES').splitlines():
            if not re.fullmatch(r'[a-zA-Z0-9_]+', table):
                raise ReleaseError('Unsupported baseline table name')
            text = self.sql('SHOW CREATE TABLE `' + table + '`')
            result[table] = digest(re.sub(r' AUTO_INCREMENT=\d+', '', text))
        return result

    def remapped_fingerprint(self, table, column, mapping):
        """Fingerprint of `table` as it must look after a declared migration rewrites
        `column` by `mapping` and changes nothing else (same row format as fingerprint)."""
        if not re.fullmatch(r'[a-zA-Z0-9_]+', table) or not re.fullmatch(r'[a-zA-Z0-9_]+', column):
            raise ReleaseError('Unsupported declared migration target')
        columns = [line.split('\t')[0] for line in self.sql('SHOW COLUMNS FROM `' + table + '`').splitlines()]
        if column not in columns or not all(re.fullmatch(r'[a-zA-Z0-9_]+', c) for c in columns):
            raise ReleaseError('Declared migration column missing: ' + table + '.' + column)
        if not all(re.fullmatch(r'[0-9]+', k) and re.fullmatch(r'[0-9]+', v) for k, v in mapping.items()):
            raise ReleaseError('Declared migration mapping must be numeric')
        # MySQL rewrites the value so the text form matches SELECT * exactly;
        # multi-line field values are therefore handled the same way as fingerprint.
        case = '`' + column + '`'
        if mapping:
            case = 'CASE CAST(`' + column + '` AS CHAR) ' + ' '.join(
                "WHEN '" + k + "' THEN '" + v + "'" for k, v in mapping.items()) + ' ELSE `' + column + '` END'
        select = ', '.join(case if c == column else '`' + c + '`' for c in columns)
        rows = self.sql('SELECT ' + select + ' FROM `' + table + '`').splitlines()
        return {'rows': len(rows), 'sha256': digest(sorted(rows))}


def identity(release):
    revision, state = release.baseline()
    web = release.ssh(['docker', 'inspect', '--format', '{{.Image}}', 'new-api-rolling-' + state['active']])
    worker = release.ssh(['docker', 'inspect', '--format', '{{.Image}}', 'new-api-rolling-worker'])
    if worker != state['worker_image']:
        raise ReleaseError('Production worker differs from its recorded identity')
    return {'revision': revision, 'web_image': web, 'worker_image': worker, 'contract': state['contract']}


def read_snapshot(release):
    from release import ROOT
    source = (ROOT / 'scripts/baseline-snapshot.py').read_bytes()
    return json.loads(release.ssh(['python3', '-', release.config['remote_root'], release.config['mysql_container']],
                                 input_data=source, timeout=300))


def verify_files(directory, manifest):
    if manifest.get('result') != 'passed' or manifest.get('scope') != SCOPE:
        raise ReleaseError('Baseline has no successful representative-data provenance')
    for name, checksum in manifest['files'].items():
        if file_hash(contained(directory, name)) != checksum:
            raise ReleaseError('Immutable baseline artifact changed: ' + name)


def sync(release):
    """Build a new frozen generation only from a stable, observed production source."""
    import bcrypt
    before = identity(release)
    expected = {'revision': release.state['plan']['baseline'], 'web_image': release.state['old_web_image'],
                'worker_image': release.state['baseline_state']['worker_image'], 'contract': release.state['baseline_state']['contract']}
    if before != expected:
        raise ReleaseError('Production changed since plan; create a new plan')
    snapshot = read_snapshot(release)
    initialization_record(snapshot)
    task_sources = resolve_task_sources(snapshot, before['revision'])
    if identity(release) != before:
        raise ReleaseError('Production changed during baseline capture')
    signature = digest({'production': before, 'snapshot': snapshot,
                        'deployment': release.state['plan']['fingerprints']['deployment']})
    directory = PRIVATE / 'baselines' / signature
    manifest_path = directory / 'manifest.json'
    if manifest_path.exists():
        manifest = read(manifest_path)
        verify_files(directory, manifest)
    else:
        if directory.exists():
            raise ReleaseError('Incomplete baseline generation retained; inspect it before retrying')
        directory.mkdir(parents=True)
        save(directory / 'snapshot.json', snapshot)
        save(directory / 'task-sources.json', task_sources)
        stack = Stack.create(directory, 'newapi-baseline-' + signature[:12], before['web_image'],
                             before['worker_image'], before['contract'], signature,
                             subnet_pool=release.config.get('validation_subnet_pool'))
        stack.start_database()
        stack.sql(snapshot['schema_sql'])
        password_hash = bcrypt.hashpw(secrets.token_hex(24).encode(), bcrypt.gensalt()).decode()
        stack.sql(seed_sql(snapshot, password_hash, task_sources))
        # The schema is captured from a running deployment; never run a newer migrator here.
        stack.compose('up', '-d', 'worker', 'web', 'gateway')
        version = stack.ready()
        stack.compose('stop', 'web', 'worker', 'gateway', timeout=160)
        logs = stack.archive_logs(directory)
        stack.dump(directory / 'database.sql')
        stack.data_archive(directory / 'data.tar.gz')
        fingerprint = stack.fingerprint()
        stack.compose('stop', 'mysql', 'redis')
        manifest = {'schema': 1, 'id': signature, 'scope': SCOPE, 'production': before,
            'captured_at': time.time(), 'result': 'passed', 'version': version, 'fingerprint': fingerprint,
            'schema_digest': digest(snapshot['schema_sql']), 'pricing_digest': digest(snapshot['pricing']),
            'setup_digest': digest(snapshot['setup']), 'initialization_verified': True,
            'task_pricing_digest': digest(snapshot['task_pricing']),
            'samples_digest': digest(snapshot['samples']), 'container_logs': logs,
            'files': {p.name: file_hash(p) for p in directory.iterdir() if p.is_file()}}
        save(manifest_path, manifest)
    # Do not advance the pointer if production changed during local preparation.
    current = read_snapshot(release)
    if identity(release) != before or any(digest(current[k]) != digest(snapshot[k]) for k in ('schema_sql', 'pricing', 'setup', 'task_pricing')):
        raise ReleaseError('Production schema/prices/initialization changed during baseline preparation')
    verify_files(directory, manifest)
    save(PRIVATE / 'baselines/current.json', {'id': signature, 'manifest_sha256': file_hash(manifest_path)})
    target = release.directory / 'fixtures'
    target.mkdir(exist_ok=True)
    for name in ('database.sql', 'data.tar.gz'):
        shutil.copy2(directory / name, target / name)
    save(target / 'pricing.json', snapshot['pricing'])
    save(release.directory / 'baseline.json', manifest)
    release.persist(baseline_generation=signature, baseline_manifest_sha256=file_hash(manifest_path),
                    pricing_digest=manifest['pricing_digest'], baseline_scope=SCOPE,
                    validation_subnet_pool=release.config.get('validation_subnet_pool'))


def require_current(release):
    manifest = read(release.directory / 'baseline.json')
    generation = contained(PRIVATE / 'baselines', release.state['baseline_generation'])
    verify_files(generation, manifest)
    if file_hash(generation / 'manifest.json') != release.state['baseline_manifest_sha256']:
        raise ReleaseError('Baseline manifest changed')
    snapshot = read_snapshot(release)
    if (identity(release) != manifest['production'] or digest(snapshot['schema_sql']) != manifest['schema_digest']
            or digest(snapshot['pricing']) != manifest['pricing_digest']
            or digest(snapshot['task_pricing']) != manifest.get('task_pricing_digest')
            or digest(initialization_record(snapshot)) != manifest.get('setup_digest')
            or manifest.get('initialization_verified') is not True):
        raise ReleaseError('Production baseline is stale; sync and validate a new clone')
