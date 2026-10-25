import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { PricingModel } from '../types'
import { isDynamicPricingModel } from './dynamic-price'
import {
  getFlatTokenPriceEntries,
  isFlatTokenPriceModel,
} from './flat-token-price'
import { getEffectiveEndpointTypes } from './model-helpers'

const model: PricingModel = {
  id: 1,
  model_name: 'qwen3.7-text-embedding',
  quota_type: 0,
  model_ratio: 37.5,
  completion_ratio: 1,
  enable_groups: ['knowledge', 'other'],
  group_ratio: { knowledge: 0.5, other: 1 },
  billing_mode: 'tiered_expr',
  billing_expr: 'tier("knowledge", p * 2 + c * 0)',
  knowledge: {
    kind: 'embedding',
    multimodal: false,
    batch_file: true,
    batch_groups: ['knowledge'],
    prices: { input: 2, batch_input: 1 },
  },
}

test('knowledge prices replace legacy chat rates and apply group discount once', () => {
  const entries = getFlatTokenPriceEntries(model, {
    tokenUnit: 'M',
    groupRatioMultiplier: 0.5,
    selectedGroup: 'knowledge',
  })
  assert.equal(isFlatTokenPriceModel(model), true)
  assert.equal(isDynamicPricingModel(model), false)
  assert.deepEqual(
    entries.map((e) => [e.key, e.value]),
    [
      ['p', 2],
      ['batch', 1],
    ]
  )
  assert.match(entries[0].formatted, /1(?:\.0+)?$/)
  assert.match(entries[1].formatted, /0\.5(?:0+)?$/)
})

test('Batch price is hidden in groups without a compatible channel', () => {
  assert.deepEqual(
    getFlatTokenPriceEntries(model, {
      tokenUnit: 'M',
      selectedGroup: 'other',
    }).map((e) => e.key),
    ['p']
  )
})

test('legacy retrieval models show input pricing and ignore chat endpoint group override', () => {
  const legacy: PricingModel = {
    ...model,
    billing_mode: 'ratio',
    billing_expr: undefined,
    knowledge: { kind: 'rerank', multimodal: false, batch_file: false },
  }
  assert.deepEqual(
    getFlatTokenPriceEntries(legacy, { tokenUnit: 'M' }).map((e) => [
      e.key,
      e.value,
    ]),
    [
      ['p', 75],
      ['c', 75],
    ]
  )
  assert.deepEqual(
    getEffectiveEndpointTypes(
      legacy,
      { knowledge: { type: 'openai', path: '/v1/chat/completions' } },
      'knowledge'
    ),
    ['jina-rerank']
  )
})

test('image pricing remains a fixed rate without output and cache columns', () => {
  const vision: PricingModel = {
    ...model,
    billing_expr: 'tier("knowledge", p * 2 + img * 4 + c * 0)',
    knowledge: {
      kind: 'embedding',
      multimodal: true,
      batch_file: false,
      prices: { input: 2, image: 4 },
    },
  }
  assert.equal(isDynamicPricingModel(vision), false)
  assert.deepEqual(
    getFlatTokenPriceEntries(vision, { tokenUnit: 'M' }).map((e) => [
      e.key,
      e.value,
    ]),
    [
      ['p', 2],
      ['img', 4],
    ]
  )
})
