import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildFlatTokenPriceExpr,
  getFlatTokenPriceEntries,
  parseFlatTokenPriceExpr,
  EMPTY_FLAT_TOKEN_PRICES,
  ratioToFlatTokenPrices,
} from './flat-token-price'

describe('flat token prices', () => {
  test('round-trips multimodal zero prices and rejects invalid optional prices', () => {
    const prices = {
      ...EMPTY_FLAT_TOKEN_PRICES,
      inputPrice: '0',
      outputPrice: '2',
      imageInputPrice: '0',
      imageOutputPrice: '3',
      audioInputPrice: '4',
      audioOutputPrice: '5',
    }
    assert.deepEqual(
      parseFlatTokenPriceExpr(buildFlatTokenPriceExpr(prices)!),
      prices
    )
    assert.equal(
      buildFlatTokenPriceExpr({ ...prices, audioInputPrice: '-1' }),
      null
    )
    assert.equal(
      buildFlatTokenPriceExpr({ ...prices, imageInputPrice: 'oops' }),
      null
    )
  })
  test('Omni output audio price uses both audio multipliers', () => {
    const prices = ratioToFlatTokenPrices({
      ratio: '1.1',
      completionRatio: '6.045454545455',
      audioRatio: '8.181818181818',
      audioCompletionRatio: '4',
    })
    assert.ok(Math.abs(Number(prices.audioInputPrice) - 18) < 1e-10)
    assert.ok(Math.abs(Number(prices.audioOutputPrice) - 72) < 1e-10)
    assert.equal(prices.imageInputPrice, '')
  })
  test('round-trips all five independently configured prices', () => {
    const expression = buildFlatTokenPriceExpr({
      inputPrice: '3',
      outputPrice: '15',
      cacheReadPrice: '1',
      cacheCreatePrice: '3.75',
      cacheCreate1hPrice: '9',
    })

    assert.equal(
      expression,
      'v2:(tier("default", p * 3 + c * 15 + cr * 1 + cc * 3.75 + cc1h * 9)\n) / 1000000'
    )
    assert.deepEqual(parseFlatTokenPriceExpr(expression || ''), {
      ...EMPTY_FLAT_TOKEN_PRICES,
      inputPrice: '3',
      outputPrice: '15',
      cacheReadPrice: '1',
      cacheCreatePrice: '3.75',
      cacheCreate1hPrice: '9',
    })
  })

  test('keeps zero prices and omits optional cache prices left blank', () => {
    const expression = buildFlatTokenPriceExpr({
      inputPrice: '0',
      outputPrice: '0',
      cacheReadPrice: '',
      cacheCreatePrice: '0',
      cacheCreate1hPrice: '',
    })

    assert.equal(
      expression,
      'v2:(tier("default", p * 0 + c * 0 + cc * 0)\n) / 1000000'
    )
    assert.deepEqual(parseFlatTokenPriceExpr(expression || ''), {
      ...EMPTY_FLAT_TOKEN_PRICES,
      inputPrice: '0',
      outputPrice: '0',
      cacheReadPrice: '',
      cacheCreatePrice: '0',
      cacheCreate1hPrice: '',
    })
  })

  test('rejects missing required prices and dynamic expressions', () => {
    assert.equal(
      buildFlatTokenPriceExpr({
        inputPrice: '3',
        outputPrice: '',
        cacheReadPrice: '1',
        cacheCreatePrice: '',
        cacheCreate1hPrice: '',
      }),
      null
    )
    assert.equal(
      parseFlatTokenPriceExpr(
        'p < 1000 ? tier("default", p * 3 + c * 15) : tier("large", p * 6 + c * 30)'
      ),
      null
    )
  })

  test('returns the configured 1h cache price for marketplace display', () => {
    const entries = getFlatTokenPriceEntries(
      {
        id: 1,
        model_name: 'claude-test',
        quota_type: 0,
        model_ratio: 0,
        completion_ratio: 0,
        enable_groups: ['default'],
        group_ratio: { default: 1 },
        billing_mode: 'flat_price',
        billing_expr:
          'tier("default", p * 3 + c * 15 + cr * 1 + cc * 3.75 + cc1h * 6)',
      },
      { tokenUnit: 'M', groupRatioMultiplier: 1 }
    )

    assert.deepEqual(
      entries.map((entry) => [entry.shortLabel, entry.value]),
      [
        ['Input', 3],
        ['Output', 15],
        ['Cache Read', 1],
        ['Cache Write (5m)', 3.75],
        ['Cache Write (1h)', 6],
      ]
    )
  })
})
