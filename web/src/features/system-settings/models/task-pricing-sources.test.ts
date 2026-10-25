import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { TaskBillingRule } from '@/features/pricing/types'
import {
  applyTaskRuleChange,
  borrowedPriceModel,
  classifyTaskSource,
  mappedModels,
  modelTaskPricingFlags,
  parseTaskExpressions,
  pluginTaskPricing,
  summarizeTaskRule,
  taskPricingOverview,
  type TaskExpressionMap,
  type TaskPricingModel,
} from './task-pricing-sources'

const rule = (
  source: TaskBillingRule['source'],
  model: string,
  mapped = model
): TaskBillingRule => ({
  source,
  plugin: 'happyhorse',
  model,
  mapped_model: mapped,
})

describe('task pricing sources', () => {
  test('classifies backend sources into the admin-facing states', () => {
    assert.equal(classifyTaskSource('plugin_original'), 'override')
    assert.equal(classifyTaskSource('plugin_mapped'), 'mapped_override')
    assert.equal(classifyTaskSource('model_original'), 'follow')
    assert.equal(classifyTaskSource('legacy_original'), 'follow')
    assert.equal(classifyTaskSource('model_mapped'), 'follow_mapped')
    assert.equal(
      classifyTaskSource('explicit_task_rule_required'),
      'needs_price'
    )
    assert.equal(classifyTaskSource('missing'), 'needs_price')
    // Retired by the backend; must never read as "fine".
    assert.equal(classifyTaskSource('unreviewed_original'), 'needs_price')
    assert.equal(classifyTaskSource('something_new'), 'needs_price')
  })

  test('an explicit plugin rule wins and the worst mapped state surfaces', () => {
    const model: TaskPricingModel = {
      model_name: 'alias',
      task_pricing: {
        happyhorse: [
          rule('model_original', 'alias', 'a'),
          rule('missing', 'alias', 'b'),
        ],
        wan: [{ ...rule('plugin_mapped', 'up', 'up'), plugin: 'wan' }],
      },
    }
    const entries = pluginTaskPricing(model, 'alias', {
      alias: { extra: 'v2:1' },
    })
    assert.deepEqual(
      entries.map((entry) => [entry.plugin, entry.status]),
      [
        ['extra', 'override'],
        ['happyhorse', 'needs_price'],
        ['wan', 'mapped_override'],
      ]
    )
    assert.deepEqual(mappedModels(entries[1], 'alias'), ['a', 'b'])
    assert.equal(borrowedPriceModel(entries[2], 'alias'), 'up')
    const overridden = pluginTaskPricing(model, 'alias', {
      alias: { happyhorse: 'v2:0.1' },
    })
    assert.equal(overridden[0].status, 'override')
    assert.equal(overridden[0].override, 'v2:0.1')
  })

  test('plugins without ready usage are unavailable rather than blocked', () => {
    const [entry] = pluginTaskPricing(
      {
        model_name: 'm',
        billing_usage: {
          status: 'conflict',
          plugins: { happyhorse: { status: 'conflict' } },
        },
      },
      'm',
      {}
    )
    assert.equal(entry.status, 'unavailable')
  })

  test('overview lists usage and rule-only models sorted by plugin then model', () => {
    const rules: TaskExpressionMap = { orphan: { happyhorse: 'v2:0' } }
    const rows = taskPricingOverview(
      [
        {
          model_name: 'zeta',
          task_pricing: { happyhorse: [rule('model_original', 'zeta')] },
        },
        { model_name: 'plain' },
        {
          model_name: 'beta',
          task_pricing: {
            happyhorse: [rule('explicit_task_rule_required', 'beta')],
          },
        },
      ],
      rules
    )
    assert.deepEqual(
      rows.map((row) => [row.model, row.status]),
      [
        ['beta', 'needs_price'],
        ['orphan', 'override'],
        ['zeta', 'follow'],
      ]
    )
    assert.deepEqual(modelTaskPricingFlags(rows), {
      beta: { override: false, needsPrice: true },
      orphan: { override: true, needsPrice: false },
      zeta: { override: false, needsPrice: false },
    })
  })

  test('rule changes touch only one plugin and drop empty models', () => {
    const rules: TaskExpressionMap = {
      a: { happyhorse: 'v2:1', wan: 'v2:2' },
      b: { happyhorse: 'v2:3' },
    }
    const set = applyTaskRuleChange(rules, 'a', 'happyhorse', 'v2:9')
    assert.deepEqual(set, {
      a: { happyhorse: 'v2:9', wan: 'v2:2' },
      b: { happyhorse: 'v2:3' },
    })
    assert.deepEqual(applyTaskRuleChange(rules, 'b', 'happyhorse', null), {
      a: { happyhorse: 'v2:1', wan: 'v2:2' },
    })
    assert.deepEqual(rules.a, { happyhorse: 'v2:1', wan: 'v2:2' })
  })

  test('parses the option defensively and summarizes rules', () => {
    assert.deepEqual(parseTaskExpressions(''), {})
    assert.deepEqual(parseTaskExpressions(undefined), {})
    assert.equal(parseTaskExpressions('{oops'), null)
    assert.equal(parseTaskExpressions('[]'), null)
    assert.deepEqual(summarizeTaskRule('v2:0.1'), {
      kind: 'expression',
      expression: 'v2:0.1',
    })
    assert.deepEqual(
      summarizeTaskRule({ version: 1, kind: 'unit_price', unit_price: 0.5 }),
      { kind: 'unit_price', unitPrice: 0.5 }
    )
    assert.deepEqual(
      summarizeTaskRule({
        version: 1,
        kind: 'token_completion',
        model_ratio: 0.25,
      }),
      { kind: 'token_completion', modelRatio: 0.25 }
    )
    assert.deepEqual(summarizeTaskRule(undefined), { kind: 'none' })
  })
})
