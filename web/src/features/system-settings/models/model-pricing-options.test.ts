import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  readModelPricing,
  updateModelPricingOptions,
} from './model-pricing-options'

test('renaming preserves all prices and inactive expressions without changing other models', () => {
  const before = {
    ModelRatio: '{"old":1.1,"other":2}',
    AudioRatio: '{"old":8.181818181818}',
    AudioCompletionRatio: '{"old":4}',
    BatchInputRatio: '{"old":0}',
    'billing_setting.billing_expr': '{"old":"inactive"}',
    'billing_setting.task_expressions':
      '{"old":{"alibaba":"v2:0.1"},"other":{"sora":"v2:0"}}',
  }
  const updates = updateModelPricingOptions(before, 'new', null, 'old')
  const after = {
    ...before,
    ...Object.fromEntries(updates.map((item) => [item.key, item.value])),
  }
  assert.equal(readModelPricing(after, 'new').knowledgeBatchInput, '0')
  assert.equal(readModelPricing(after, 'new').audioCompletionRatio, '4')
  assert.equal(readModelPricing(after, 'new').billingMode, 'per-token')
  assert.equal(readModelPricing(after, 'new').billingExpr, 'inactive')
  assert.equal(JSON.parse(after.ModelRatio).other, 2)
  assert.equal(JSON.parse(after.ModelRatio).old, undefined)
  assert.deepEqual(JSON.parse(after['billing_setting.task_expressions']), {
    new: { alibaba: 'v2:0.1' },
    other: { sora: 'v2:0' },
  })
  assert.deepEqual(updateModelPricingOptions(after, 'new'), [])
})

test('ordinary price edits preserve plugin overrides', () => {
  const before = {
    'billing_setting.task_expressions': '{"model":{"alibaba":"v2:0.1"}}',
  }
  const updates = updateModelPricingOptions(before, 'model', {
    name: 'model',
    billingMode: 'per-request',
    price: '0.2',
  })
  assert.equal(
    updates.find((item) => item.key === 'billing_setting.task_expressions'),
    undefined
  )
})

test('new undetected model saves multimodal expression and Batch in the same update', () => {
  const updates = updateModelPricingOptions({}, 'custom', {
    name: 'custom',
    billingMode: 'flat_price',
    billingExpr: 'tier("default", p * 0 + c * 2 + img * 3 + ai * 4 + ao * 5)',
    knowledgeBatchInput: '0',
  })
  const after = Object.fromEntries(
    updates.map((item) => [item.key, item.value])
  )
  const pricing = readModelPricing(after, 'custom')
  assert.equal(pricing.billingMode, 'flat_price')
  assert.match(pricing.billingExpr!, /img \* 3/)
  assert.equal(pricing.knowledgeBatchInput, '0')
  assert.equal(pricing.ratio, '')
})

test('audio seconds stays in seconds when its price is edited', () => {
  const updates = updateModelPricingOptions({}, 'asr', {
    name: 'asr',
    billingMode: 'audio_seconds',
    ratio: '100',
  })
  const pricing = readModelPricing(
    Object.fromEntries(updates.map((item) => [item.key, item.value])),
    'asr'
  )
  assert.equal(pricing.billingMode, 'audio_seconds')
  assert.equal(pricing.ratio, '100')
})
