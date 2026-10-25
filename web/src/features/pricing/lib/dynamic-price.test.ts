import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from './dynamic-price'

const model: PricingModel = {
  id: 1,
  model_name: 'gpt-5.6-sol',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: ['default'],
  billing_mode: 'tiered_expr',
  billing_expr:
    'len <= 272000 ? tier("standard", p * 5 + c * 30 + cr * 0.5) : tier("long_context", p * 10 + c * 45 + cr * 1 + cc * 12.5)',
}

describe('getDynamicPricingSummary', () => {
  test('preserves every tier and the union of their price fields', () => {
    const summary = getDynamicPricingSummary(model, { tokenUnit: 'M' })

    assert.ok(summary)
    assert.equal(summary.tierRows.length, 2)
    assert.deepEqual(
      summary.tierRows.map((row) => row.tier.label),
      ['standard', 'long_context']
    )
    assert.deepEqual(
      summary.priceFields.map((entry) => entry.field),
      ['inputPrice', 'outputPrice', 'cacheReadPrice', 'cacheCreatePrice']
    )

    const standard = new Map(
      summary.tierRows[0].entries.map((entry) => [entry.field, entry.value])
    )
    const longContext = new Map(
      summary.tierRows[1].entries.map((entry) => [entry.field, entry.value])
    )

    assert.equal(standard.has('cacheCreatePrice'), false)
    assert.equal(longContext.get('cacheCreatePrice'), 12.5)
  })

  test('combines the active time multiplier with a zero group ratio', () => {
    const zeroRatioModel: PricingModel = {
      ...model,
      enable_groups: ['3_welfare_china'],
      group_ratio: { '3_welfare_china': 0 },
      billing_expr:
        'tier("base", p * 5 + c * 30) * (hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 22 ? 2 : 1)',
    }
    const groupRatio = getDynamicDisplayGroupRatio(
      zeroRatioModel,
      '3_welfare_china'
    )
    const summary = getDynamicPricingSummary(zeroRatioModel, {
      tokenUnit: 'M',
      groupRatioMultiplier: groupRatio,
      now: new Date('2026-08-31T12:00:00Z'),
    })

    assert.equal(groupRatio, 0)
    assert.equal(summary?.timePricing?.currentMultiplier, 2)
    assert.equal(summary?.currentEffectiveMultiplier, 0)
  })

  test('falls back to one only when the group ratio is missing', () => {
    assert.equal(getDynamicDisplayGroupRatio(model, 'default'), 1)
  })
})
