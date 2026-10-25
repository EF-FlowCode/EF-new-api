"""Compare complete pricing snapshots without changing prices or group ratios."""
FIELDS = {'ModelRatio': 'input_ratio', 'CompletionRatio': 'output_ratio', 'CacheRatio': 'cache_read_ratio',
          'CreateCacheRatio': 'cache_write_ratio', 'ImageRatio': 'image_input_ratio', 'AudioRatio': 'audio_input_ratio',
          'AudioCompletionRatio': 'audio_output_ratio', 'BatchInputRatio': 'batch_input_ratio', 'ModelPrice': 'unit_price',
          'billing_setting.billing_mode': 'mode', 'billing_setting.billing_expr': 'expression',
          'billing_setting.task_expressions': 'task_expressions'}


def canonical(snapshot):
    if 'ModelPricing' in snapshot:
        return snapshot['ModelPricing']['models']
    models = {}
    for key, field in FIELDS.items():
        for model, value in snapshot.get(key, {}).items():
            models.setdefault(model, {})[field] = value
    return models


def amount_expression(expression):
    """Accept only the exact whole-result conversion emitted by the migration."""
    if not expression.strip():
        return expression
    text = expression.strip()
    if text.startswith('v2:'):
        return text
    body = text[3:] if text.startswith('v1:') else text
    return 'v2:(' + body + '\n) / 1000000'


def compare(before, after):
    assert before.get('ModelPricing', {}).get('task_priority_version', 0) == after.get('ModelPricing', {}).get('task_priority_version', 0), 'Task pricing priority changed; a complete task migration acceptance is required'
    old, new = canonical(before), canonical(after)
    assert set(old) == set(new), 'Pricing model set changed'
    changes = []
    unit_changes = []
    fields = 0
    for model, prices in old.items():
        assert set(prices) == set(new[model]), 'Pricing dimensions changed: ' + model
        for field, value in prices.items():
            fields += 1
            if new[model][field] != value:
                if (field == 'expression' and after.get('ModelPricing', {}).get('version') == 2
                        and new[model][field] == amount_expression(value)):
                    unit_changes.append({'model': model, 'before': value, 'after': new[model][field]})
                    continue
                if field == 'task_expressions' and new[model][field] == {
                    plugin: ({'version': 1, 'kind': 'amount', 'expression': rule}
                             if isinstance(rule, str) else rule)
                    for plugin, rule in value.items()
                }:
                    unit_changes.append({'model': model, 'field': field, 'before': value, 'after': new[model][field]})
                    continue
                allowed = ('ModelPricing' not in before and model in ('qwen3-vl-embedding', 'qwen3-vl-rerank')
                           and field == 'mode' and value == 'tiered_expr' and new[model][field] == 'flat_price')
                assert allowed, 'Price changed: ' + model + '/' + field
                changes.append({'model': model, 'field': field, 'before': value, 'after': new[model][field]})
    for group in ('GroupRatio', 'GroupGroupRatio', 'QuotaPerUnit', 'SelfUseModeEnabled', 'ModelPricingTaskMigrationV1'):
        assert before.get(group) == after.get(group), 'Group multiplier changed'
    for key in ('UserUsableGroups', 'group_ratio_setting.group_special_usable_group', 'AutoGroups', 'DefaultUseAutoGroup'):
        assert before.get(key) == after.get(key), 'Group authorization/routing changed: ' + key
    assert not any(key in after for key in FIELDS), 'Legacy pricing storage remains'
    return {'models': len(new), 'fields': fields, 'classification_changes': changes,
            'amount_unit_changes': unit_changes,
            'prices_and_groups_preserved': True, 'group_authorization_preserved': True, 'legacy_option_rows': 0}
