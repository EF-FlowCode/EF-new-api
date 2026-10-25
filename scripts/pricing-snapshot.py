"""Read only pricing and group options via an existing MySQL container."""
import json
from pathlib import Path
import re
import subprocess
import sys

KEYS = ['ModelPricing', 'ModelRatio', 'CompletionRatio', 'CacheRatio', 'CreateCacheRatio',
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


def main(root, mysql):
    config = json.loads((Path(root) / '.rolling/compose.json').read_text())
    dsn = config['services']['worker']['environment']['SQL_DSN']
    match = re.fullmatch(r'([^:]+):(.*)@tcp\([^)]+\)/([^?]+)(?:\?.*)?', dsn)
    if not match:
        raise RuntimeError('Unsupported DSN')
    user, password, database = match.groups()
    query = 'SET SESSION MAX_EXECUTION_TIME=5000; START TRANSACTION READ ONLY; SELECT `key`,HEX(`value`) FROM options WHERE `key` IN (' + ','.join("'" + k + "'" for k in KEYS) + ') ORDER BY `key`; COMMIT;'
    result = subprocess.run(['docker', 'exec', '-e', 'MYSQL_PWD=' + password, mysql,
        'mysql', '-u' + user, '--default-character-set=utf8mb4', '--batch', '--raw', '--skip-column-names', database,
        '-e', query], capture_output=True, check=True)
    snapshot = {k: bytes.fromhex(v).decode() for k, v in (line.split('\t', 1) for line in result.stdout.decode().splitlines())}
    if 'ModelPricing' not in snapshot and 'ModelRatio' not in snapshot:
        raise RuntimeError('Incomplete pricing snapshot')
    print(json.dumps(sanitize_migration_provenance(snapshot), ensure_ascii=False))


if __name__ == '__main__':
    try:
        main(sys.argv[1], sys.argv[2])
    except Exception:
        sys.exit('Pricing read failed; credentials and row contents suppressed')
