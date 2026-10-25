"""Read-only production schema/pricing and allowlisted, de-identified samples.

Executed over SSH stdin. Never writes a file or row on the server. Credentials,
original identities, provider keys, request bodies and task payloads never leave it.
"""
import json
from pathlib import Path
import re
import subprocess
import sys

PRICING_KEYS = ['ModelPricing', 'ModelRatio', 'CompletionRatio', 'CacheRatio', 'CreateCacheRatio',
    'ImageRatio', 'AudioRatio', 'AudioCompletionRatio', 'BatchInputRatio', 'ModelPrice',
    'billing_setting.billing_mode', 'billing_setting.billing_expr', 'billing_setting.knowledge_prices',
    'GroupRatio', 'GroupGroupRatio', 'QuotaPerUnit', 'UserUsableGroups',
    'group_ratio_setting.group_special_usable_group', 'AutoGroups', 'DefaultUseAutoGroup',
    'SelfUseModeEnabled', 'ModelPricingTaskMigrationV1',
    # One-time migration markers: without them a clone re-runs the migration on startup.
    'ChannelTypeRenumberV1', 'LegacyAccessTokenRetireAt']


def sanitize_migration_provenance(pricing):
    # Historical source/target hashes and original prices are retained. Channel
    # identities in historical audit entries are not part of clone routing.
    key = 'ModelPricingTaskMigrationV1'
    if key in pricing:
        record = json.loads(pricing[key])
        if not record.get('source_hash') or not record.get('target_hash') or not record.get('original'):
            raise RuntimeError('Incomplete task migration provenance')
        for entry in record.get('entries', []):
            entry.pop('channel_id', None)
        pricing[key] = json.dumps(record, ensure_ascii=False, separators=(',', ':'))
    document = json.loads(pricing.get('ModelPricing', '{}'))
    if document.get('task_priority_version', 0) == 1 and key not in pricing:
        raise RuntimeError('Migrated pricing requires provenance')
    return pricing


def sanitize_task_pricing(data):
    """Keep routing contracts, replace channel/plugin IDs, export no source/keys."""
    channels = sorted(data['channels'], key=lambda row: row['id'])
    bindings = sorted(data['bindings'], key=lambda row: row['channel_id'])
    # Dangling bindings remain dangling; never accidentally attach one to a sample.
    ids = sorted({row['id'] for row in channels} | {row['channel_id'] for row in bindings})
    aliases = {old: index for index, old in enumerate(ids, 1000)}
    return {'schema': 1,
        'channels': [dict(row, id=aliases[row['id']]) for row in channels],
        'bindings': [dict(row, channel_id=aliases[row['channel_id']]) for row in bindings],
        'control': data['control'], 'plugins': data['plugins'], 'sources': data['sources']}


def normalize_schema(value):
    # The next insert ID is live data, not a schema change.
    return re.sub(r' AUTO_INCREMENT=\d+', '', value).replace('\r\n', '\n')


def main(root, mysql):
    config = json.loads((Path(root) / '.rolling/compose.json').read_text())
    dsn = config['services']['worker']['environment']['SQL_DSN']
    match = re.fullmatch(r'([^:]+):(.*)@tcp\([^)]+\)/([^?]+)(?:\?.*)?', dsn)
    if not match:
        raise RuntimeError('Unsupported DSN')
    user, password, database = match.groups()
    base = ['docker', 'exec', '-e', 'MYSQL_PWD=' + password, mysql]
    def sql(query):
        return subprocess.run(base + ['mysql', '-u' + user, '--default-character-set=utf8mb4',
            '--batch', '--raw', '--skip-column-names', database, '-e', query],
            capture_output=True, check=True, timeout=30).stdout.decode('utf-8').strip()
    def schema():
        result = subprocess.run(base + ['mysqldump', '-u' + user, '--no-data', '--skip-comments',
            '--skip-lock-tables', '--no-tablespaces', '--skip-triggers', '--set-gtid-purged=OFF', database],
            capture_output=True, check=True, timeout=120)
        return normalize_schema(result.stdout.decode('utf-8'))
    before = schema()
    # Preserve financial values but export no source IDs, names, keys or content.
    queries = {
        'users': "SELECT quota,used_quota,request_count,status FROM users ORDER BY id LIMIT 4",
        'tokens': "SELECT remain_quota,used_quota,unlimited_quota,status FROM tokens ORDER BY id LIMIT 4",
        'logs': "SELECT quota,prompt_tokens,completion_tokens,type FROM logs ORDER BY id DESC LIMIT 5",
        'tasks': "SELECT quota,status,platform FROM tasks WHERE status IN ('SUCCESS','FAILURE') ORDER BY id DESC LIMIT 4",
    }
    columns = {
        'users': ['quota', 'used_quota', 'request_count', 'status'],
        'tokens': ['remain_quota', 'used_quota', 'unlimited_quota', 'status'],
        'logs': ['quota', 'prompt_tokens', 'completion_tokens', 'type'],
        'tasks': ['quota', 'status', 'platform'],
    }
    statements = ['SET SESSION MAX_EXECUTION_TIME=5000', 'START TRANSACTION READ ONLY',
        "SELECT 'pricing',JSON_OBJECTAGG(`key`,`value`) FROM options WHERE `key` IN (" +
        ','.join("'" + k + "'" for k in PRICING_KEYS) + ')']
    for name, query in queries.items():
        pairs = ','.join("'" + col + "',`" + col + '`' for col in columns[name])
        statements.append("SELECT '" + name + "',COALESCE(JSON_ARRAYAGG(JSON_OBJECT(" + pairs +
                          ")),JSON_ARRAY()) FROM (" + query + ') AS sample')
    # Same read-only transaction as prices, including paused channels/bindings.
    # Explicit allowlists prevent credentials, names and arbitrary source from export.
    task_queries = {
        'channels': "SELECT id,type,status,models,model_mapping,`group` FROM channels ORDER BY id",
        'bindings': "SELECT channel_id,`key`,CAST(version AS CHAR) AS version,source_hash,factory,enabled FROM task_plugin_production_bindings ORDER BY channel_id",
        'control': "SELECT revision,enabled,factory_enabled,disabled_factory FROM task_plugin_controls WHERE id=1",
        'plugins': "SELECT `key`,api_version,CAST(version AS CHAR) AS version,source_hash,enabled,active,SHA2(source,256) AS actual_source_hash FROM task_plugins ORDER BY `key`,version",
        'sources': "SELECT `key`,api_version,CAST(version AS CHAR) AS version,CAST(source_hash AS CHAR) AS source_hash,SHA2(source,256) AS actual_source_hash FROM task_plugin_source_snapshots WHERE source_hash IN (SELECT source_hash FROM task_plugin_production_bindings) ORDER BY source_hash",
    }
    task_columns = {
        'channels': ['id', 'type', 'status', 'models', 'model_mapping', 'group'],
        'bindings': ['channel_id', 'key', 'version', 'source_hash', 'factory', 'enabled'],
        'control': ['revision', 'enabled', 'factory_enabled', 'disabled_factory'],
        'plugins': ['key', 'api_version', 'version', 'source_hash', 'enabled', 'active', 'actual_source_hash'],
        'sources': ['key', 'api_version', 'version', 'source_hash', 'actual_source_hash'],
    }
    for name, query in task_queries.items():
        pairs = ','.join("'" + col + "',`" + col + '`' for col in task_columns[name])
        statements.append("SELECT 'task_" + name + "',COALESCE(JSON_ARRAYAGG(JSON_OBJECT(" + pairs +
                          ")),JSON_ARRAY()) FROM (" + query + ') AS task_config')
    # Installation state is required to exercise an existing deployment's UI.
    # Export no source ID; the local baseline assigns its own singleton ID.
    statements.append("SELECT 'setup',JSON_OBJECT('version',version,'initialized_at',initialized_at) "
                      "FROM setups ORDER BY id LIMIT 1")
    statements.append('COMMIT')
    data = {name: json.loads(value) for name, value in
            (line.split('\t', 1) for line in sql(';'.join(statements)).splitlines())}
    if before != schema():
        raise RuntimeError('Production schema changed during snapshot')
    if not data.get('pricing') or not {'ModelPricing', 'ModelRatio'}.intersection(data['pricing']):
        raise RuntimeError('Missing pricing configuration')
    if not data.get('setup'):
        raise RuntimeError('Missing production initialization record')
    setup = data.pop('setup')
    task_pricing = sanitize_task_pricing({name: data.pop('task_' + name) for name in task_queries})
    print(json.dumps({'schema_sql': before, 'pricing': sanitize_migration_provenance(data.pop('pricing')), 'setup': setup, 'samples': data,
        'task_pricing': task_pricing,
        'option_keys': PRICING_KEYS,
        'scope': 'production_schema_pricing_sanitized_samples',
        'sample_policy': 'allowlisted financial/init/routing/plugin metadata; synthetic identities and local-only endpoints; no provider credentials or script source',
        'mysql_version': sql('SELECT VERSION()')}, ensure_ascii=False))


if __name__ == '__main__':
    try:
        main(sys.argv[1], sys.argv[2])
    except Exception as error:
        sys.exit('Read-only baseline snapshot failed: ' + type(error).__name__ + '; no credentials or row contents disclosed')
