import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { readPricingConfig } from './model-mutate-drawer'

describe('readPricingConfig', () => {
  test('loads existing pricing when creating a model with the same name', () => {
    const pricing = readPricingConfig(
      {
        ModelPrice: '{}',
        ModelRatio: '{"existing-model":1.5}',
        CacheRatio: '{"existing-model":0}',
        CompletionRatio: '{"existing-model":2}',
        ImageRatio: '{}',
        AudioRatio: '{}',
        AudioCompletionRatio: '{}',
      },
      'existing-model'
    )

    assert.deepEqual(pricing, {
      mode: 'per-token',
      fields: {
        price: '',
        ratio: '1.5',
        cacheRatio: '0',
        completionRatio: '2',
        imageRatio: '',
        audioRatio: '',
        audioCompletionRatio: '',
      },
      promptPrice: '3',
      completionPrice: '6',
      advancedOpen: true,
    })
  })
})
