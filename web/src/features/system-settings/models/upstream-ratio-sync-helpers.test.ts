import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  PRICING_OPTION_KEYS,
  applyPricingSyncResolutions,
  buildPricingSyncUpdates,
  getConfiguredBillingCategory,
  type PricingOptionRecords,
} from './upstream-ratio-sync-helpers'

function pricingOptions(
  overrides: Partial<PricingOptionRecords> = {}
): PricingOptionRecords {
  return Object.fromEntries(
    PRICING_OPTION_KEYS.map((key) => [key, { ...(overrides[key] || {}) }])
  ) as PricingOptionRecords
}

describe('upstream pricing sync billing compatibility', () => {
  test('ordinary ratios replace expression billing for the selected model only', () => {
    const current = pricingOptions({
      ModelRatio: { legacy: 1, untouched: 9, audio: 2 },
      CompletionRatio: { legacy: 2 },
      ModelPrice: { legacy: 0.5 },
      'billing_setting.billing_mode': {
        legacy: 'flat_price',
        dynamic: 'tiered_expr',
        audio: 'audio_seconds',
      },
      'billing_setting.billing_expr': {
        legacy: 'tier("base", p * 3 + c * 15)',
        dynamic:
          'len < 100 ? tier("small", p * 1 + c * 2) : tier("large", p * 2 + c * 4)',
        audio: 'tier("stale", p * 99 + c * 99)',
      },
    })

    assert.equal(getConfiguredBillingCategory('legacy', current), 'expression')

    const next = applyPricingSyncResolutions(current, {
      legacy: { model_ratio: 3, completion_ratio: 5 },
      audio: { model_ratio: 4 },
    })

    assert.equal(next.ModelRatio.legacy, 3)
    assert.equal(next.CompletionRatio.legacy, 5)
    assert.equal(next.ModelPrice.legacy, undefined)
    assert.equal(next['billing_setting.billing_mode'].legacy, undefined)
    assert.equal(next['billing_setting.billing_expr'].legacy, undefined)
    assert.equal(next.ModelRatio.untouched, 9)
    assert.equal(next['billing_setting.billing_mode'].dynamic, 'tiered_expr')
    assert.equal(next['billing_setting.billing_mode'].audio, 'audio_seconds')
    assert.equal(next['billing_setting.billing_expr'].audio, undefined)
  })

  test('expression billing removes ratios and per-call price', () => {
    const current = pricingOptions({
      ModelRatio: { model: 1 },
      CompletionRatio: { model: 2 },
      CacheRatio: { model: 0.1 },
      CreateCacheRatio: { model: 1.25 },
      ImageRatio: { model: 3 },
      AudioRatio: { model: 4 },
      AudioCompletionRatio: { model: 5 },
      ModelPrice: { model: 0.5 },
    })

    const next = applyPricingSyncResolutions(current, {
      model: {
        billing_mode: 'flat_price',
        billing_expr: 'tier("base", p * 3 + c * 15)',
      },
    })

    for (const key of PRICING_OPTION_KEYS.slice(0, 8)) {
      assert.equal(next[key].model, undefined)
    }
    assert.equal(next['billing_setting.billing_mode'].model, 'flat_price')
    assert.equal(
      next['billing_setting.billing_expr'].model,
      'tier("base", p * 3 + c * 15)'
    )
  })

  test('per-call price removes ratios and expression billing', () => {
    const current = pricingOptions({
      ModelRatio: { model: 1 },
      CacheRatio: { model: 0.1 },
      'billing_setting.billing_mode': { model: 'tiered_expr' },
      'billing_setting.billing_expr': {
        model: 'tier("base", p * 3 + c * 15)',
      },
    })

    const next = applyPricingSyncResolutions(current, {
      model: { model_price: 0.25 },
    })

    assert.equal(next.ModelPrice.model, 0.25)
    assert.equal(next.ModelRatio.model, undefined)
    assert.equal(next.CacheRatio.model, undefined)
    assert.equal(next['billing_setting.billing_mode'].model, undefined)
    assert.equal(next['billing_setting.billing_expr'].model, undefined)
  })

  test('builds one complete payload for the atomic bulk endpoint', () => {
    const updates = buildPricingSyncUpdates(pricingOptions(), {
      model: { model_ratio: 2 },
    })

    assert.deepEqual(
      updates.map((update) => update.key),
      [...PRICING_OPTION_KEYS]
    )
    assert.equal(
      JSON.parse(updates.find((update) => update.key === 'ModelRatio')!.value)
        .model,
      2
    )
  })
})
