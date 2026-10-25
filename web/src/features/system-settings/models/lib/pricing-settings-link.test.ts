import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  PRICING_SETTINGS_PATH,
  pricingModelFromSearch,
  pricingSettingsUrl,
} from './pricing-settings-link'

describe('pricing settings link', () => {
  it('points at the current settings route, not the retired /console path', () => {
    assert.equal(PRICING_SETTINGS_PATH, '/system-settings/models/ratio')
    assert.equal(pricingSettingsUrl(), '/system-settings/models/ratio')
  })

  it('carries the model name and round-trips it', () => {
    const url = pricingSettingsUrl(' kimi-k2.8-preview ')
    assert.equal(url, '/system-settings/models/ratio?model=kimi-k2.8-preview')
    assert.equal(
      pricingModelFromSearch(url.slice(url.indexOf('?'))),
      'kimi-k2.8-preview'
    )
    assert.equal(
      pricingModelFromSearch('?model=' + encodeURIComponent('a/b c')),
      'a/b c'
    )
  })

  it('ignores a missing or blank model', () => {
    assert.equal(pricingModelFromSearch(''), null)
    assert.equal(pricingModelFromSearch('?model=%20'), null)
  })
})
