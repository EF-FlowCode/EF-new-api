import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { SORT_OPTIONS } from '../constants'
import type { PricingModel } from '../types'
import { getSortPrice, sortModels } from './filters'

function pricingModel(
  modelName: string,
  overrides: Partial<PricingModel>
): PricingModel {
  return {
    id: 1,
    model_name: modelName,
    quota_type: 0,
    model_ratio: 1,
    completion_ratio: 1,
    enable_groups: ['default'],
    ...overrides,
  } as PricingModel
}

// 输入价（$/1M）：legacy-ratio 2、expr-tokens 1、expr-long-context 3、expr-input-only 0.1、flat-expr 0.8。
const models = [
  pricingModel('legacy-ratio', { model_ratio: 1, completion_ratio: 4 }),
  pricingModel('legacy-per-call', { quota_type: 1, model_price: 0.05 }),
  pricingModel('cheap-per-call', { quota_type: 1, model_price: 0.01 }),
  pricingModel('expr-tokens', {
    billing_mode: 'tiered_expr',
    billing_expr: 'tier("base", p * 1 + c * 10)',
  }),
  pricingModel('expr-long-context', {
    billing_mode: 'tiered_expr',
    billing_expr:
      'len <= 200000 ? tier("standard", p * 3 + c * 15) : tier("long", p * 6 + c * 22.5)',
  }),
  pricingModel('expr-input-only', {
    billing_mode: 'tiered_expr',
    billing_expr: 'len <= 1000 ? tier("a", p * 0.1) : tier("b", p * 0.2)',
  }),
  pricingModel('flat-expr', {
    billing_mode: 'flat_price',
    billing_expr: 'tier("default", p * 0.8 + c * 2.7)',
  }),
  pricingModel('audio-seconds', {
    billing_mode: 'audio_seconds',
    model_ratio: 0.001,
  }),
]

const order = (sortBy: string) =>
  sortModels(models, sortBy).map((m) => m.model_name)

describe('pricing sort by price kind', () => {
  test('input price compares base prices across ratio, flat and expression billing', () => {
    assert.deepEqual(order(SORT_OPTIONS.INPUT_PRICE_LOW), [
      'expr-input-only',
      'flat-expr',
      'expr-tokens',
      'legacy-ratio',
      'expr-long-context',
      'legacy-per-call',
      'cheap-per-call',
      'audio-seconds',
    ])
    assert.deepEqual(order(SORT_OPTIONS.INPUT_PRICE_HIGH).slice(0, 5), [
      'expr-long-context',
      'legacy-ratio',
      'expr-tokens',
      'flat-expr',
      'expr-input-only',
    ])
  })

  test('output price keeps models without an output price last', () => {
    assert.deepEqual(order(SORT_OPTIONS.OUTPUT_PRICE_LOW), [
      'flat-expr',
      'legacy-ratio',
      'expr-tokens',
      'expr-long-context',
      'legacy-per-call',
      'cheap-per-call',
      'expr-input-only',
      'audio-seconds',
    ])
  })

  test('per-request price only ranks per-request models, unpriced last in both directions', () => {
    assert.deepEqual(order(SORT_OPTIONS.REQUEST_PRICE_LOW).slice(0, 2), [
      'cheap-per-call',
      'legacy-per-call',
    ])
    assert.deepEqual(order(SORT_OPTIONS.REQUEST_PRICE_HIGH).slice(0, 2), [
      'legacy-per-call',
      'cheap-per-call',
    ])
  })

  test('sorting does not mutate the input and unknown options keep the order', () => {
    const before = models.map((m) => m.model_name)
    sortModels(models, SORT_OPTIONS.INPUT_PRICE_HIGH)
    assert.deepEqual(
      models.map((m) => m.model_name),
      before
    )
    assert.deepEqual(order('price-low'), before)
  })

  test('sort price reads the card base price', () => {
    assert.equal(getSortPrice(models[0], 'output'), 8)
    assert.equal(getSortPrice(models[1], 'input'), undefined)
    assert.equal(getSortPrice(models[3], 'request'), undefined)
    assert.equal(getSortPrice(models[7], 'input'), undefined)
  })
})
