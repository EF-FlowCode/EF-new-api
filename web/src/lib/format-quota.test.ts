import assert from 'node:assert/strict'
import { afterEach, describe, test } from 'node:test'
import {
  DEFAULT_CURRENCY_CONFIG,
  useSystemConfigStore,
} from '@/stores/system-config-store'
import {
  getEditableQuotaStep,
  parseQuotaFromDollars,
  quotaUnitsToEditableAmount,
} from './format'

afterEach(() => {
  useSystemConfigStore.getState().setConfig({
    currency: { ...DEFAULT_CURRENCY_CONFIG },
  })
})

describe('editable quota conversion', () => {
  test('removes floating-point noise from converted CNY amounts', () => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'CNY',
        usdExchangeRate: 7.2,
      },
    })

    assert.equal(quotaUnitsToEditableAmount(13_888_889), 200)
    assert.equal(getEditableQuotaStep(), 0.0001)
  })

  test('uses raw quota units when token display is enabled', () => {
    useSystemConfigStore.getState().setConfig({
      currency: {
        ...DEFAULT_CURRENCY_CONFIG,
        quotaDisplayType: 'TOKENS',
      },
    })

    assert.equal(quotaUnitsToEditableAmount(500_001), 500_001)
    assert.equal(parseQuotaFromDollars(500_001), 500_001)
    assert.equal(getEditableQuotaStep(), 1)
  })
})
