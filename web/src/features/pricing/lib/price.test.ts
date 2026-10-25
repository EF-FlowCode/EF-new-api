import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import { formatFixedPrice, formatGroupPrice } from './price'

const tokenModel: PricingModel = {
  id: 1,
  model_name: 'zero-ratio-token-model',
  quota_type: 0,
  model_ratio: 1,
  completion_ratio: 1,
  enable_groups: ['zero'],
}

const requestModel: PricingModel = {
  ...tokenModel,
  model_name: 'zero-ratio-request-model',
  quota_type: 1,
  model_price: 3,
}

describe('group price formatting', () => {
  test('preserves an explicit zero group ratio', () => {
    assert.equal(
      formatGroupPrice(tokenModel, 'zero', 'input', 'M', false, 1, 1, {
        zero: 0,
      }),
      '$0'
    )
    assert.equal(
      formatFixedPrice(requestModel, 'zero', false, 1, 1, { zero: 0 }),
      '$0'
    )
  })

  test('falls back to one when the group ratio is missing', () => {
    assert.equal(
      formatGroupPrice(tokenModel, 'missing', 'input', 'M', false, 1, 1, {}),
      '$2'
    )
    assert.equal(
      formatFixedPrice(requestModel, 'missing', false, 1, 1, {}),
      '$3'
    )
  })
})
