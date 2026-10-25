import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import { getDimensionalPricingSummary } from './dimensional-price'

const model: PricingModel = {
  id: 1,
  model_name: 'happyhorse-1.0-t2v',
  quota_type: 1,
  model_ratio: 0,
  completion_ratio: 0,
  model_price: 0.36,
  enable_groups: ['0_Tools_Media'],
  billing_mode: 'dimensional',
  dimensional_pricing: {
    unit: 'second',
    dimension: 'resolution',
    sample_quantity: 5,
    tiers: [
      { key: '720P', label: '720P', ratio: 1 },
      { key: '1080P', label: '1080P', ratio: 1.778 },
    ],
  },
}

describe('getDimensionalPricingSummary', () => {
  test('computes per-second tiers and a five-second reference from model price', () => {
    const summary = getDimensionalPricingSummary(model, {
      groupRatioMultiplier: 1,
    })

    assert.ok(summary)
    assert.equal(summary.unit, 'second')
    assert.equal(summary.dimension, 'resolution')
    assert.equal(summary.sampleQuantity, 5)
    assert.deepEqual(
      summary.rows.map((row) => row.key),
      ['720P', '1080P']
    )
    assert.ok(Math.abs(summary.rows[0].unitPrice - 0.36) < 1e-9)
    assert.ok(Math.abs(summary.rows[0].samplePrice - 1.8) < 1e-9)
    assert.ok(Math.abs(summary.rows[1].unitPrice - 0.64008) < 1e-9)
    assert.ok(Math.abs(summary.rows[1].samplePrice - 3.2004) < 1e-9)
  })
})
