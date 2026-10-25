import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { EMPTY_FLAT_TOKEN_PRICES } from '@/features/pricing/lib/flat-token-price'
import { readPricingConfig } from './model-mutate-drawer'

describe('readPricingConfig', () => {
  test('loads existing pricing when creating a model with the same name', () => {
    const pricing = readPricingConfig(
      {
        ModelPrice: '{}',
        ModelRatio: '{"existing-model":1.5}',
        CacheRatio: '{"existing-model":0}',
        CreateCacheRatio: '{"existing-model":1.25}',
        CompletionRatio: '{"existing-model":2}',
        ImageRatio: '{}',
        AudioRatio: '{}',
        AudioCompletionRatio: '{}',
        'billing_setting.billing_mode': '{}',
        'billing_setting.task_expressions': '{}',
        'billing_setting.billing_expr': '{}',
      },
      'existing-model'
    )

    assert.deepEqual(pricing, {
      mode: 'per-token',
      subMode: 'ratio',
      fields: {
        price: '',
        ratio: '1.5',
        cacheRatio: '0',
        createCacheRatio: '1.25',
        completionRatio: '2',
        imageRatio: '',
        audioRatio: '',
        audioCompletionRatio: '',
      },
      promptPrice: '3',
      flatPrices: { ...EMPTY_FLAT_TOKEN_PRICES },
      advancedOpen: true,
    })
  })

  test('loads all flat token prices including 1h cache writes', () => {
    const pricing = readPricingConfig(
      {
        ModelPrice: '{}',
        ModelRatio: '{}',
        CacheRatio: '{}',
        CreateCacheRatio: '{}',
        CompletionRatio: '{}',
        ImageRatio: '{}',
        AudioRatio: '{}',
        AudioCompletionRatio: '{}',
        'billing_setting.billing_mode': '{"claude-test":"flat_price"}',
        'billing_setting.task_expressions': '{}',
        'billing_setting.billing_expr':
          '{"claude-test":"tier(\\"default\\", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)"}',
      },
      'claude-test'
    )

    assert.equal(pricing.mode, 'per-token')
    assert.equal(pricing.subMode, 'price')
    assert.deepEqual(pricing.flatPrices, {
      ...EMPTY_FLAT_TOKEN_PRICES,
      inputPrice: '3',
      outputPrice: '15',
      cacheReadPrice: '0.3',
      cacheCreatePrice: '3.75',
      cacheCreate1hPrice: '6',
    })
  })
})
