import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { LogOtherData } from '../types'
import { getTieredBillingSummary, getUnresolvedTierText } from './format'

const zh: Record<string, string> = {
  'Special billing expression': '特殊计费表达式',
  standard: '标准',
}

// 模拟 i18next 的 t：命中翻译用译文，否则用 defaultValue，再否则回落 key。
const t = (key: string, opts?: Record<string, unknown>) =>
  zh[key] ?? (opts?.defaultValue as string | undefined) ?? key

const b64 = (s: string) => Buffer.from(s).toString('base64')

describe('unresolved billing tier text', () => {
  test('keeps the recorded tier instead of a no-match message', () => {
    assert.equal(
      getUnresolvedTierText({ matched_tier: 'recorded' }, t),
      'recorded'
    )
    assert.equal(getUnresolvedTierText({ matched_tier: ' music ' }, t), 'music')
  })

  test('translates a known recorded tier name', () => {
    assert.equal(getUnresolvedTierText({ matched_tier: 'standard' }, t), '标准')
  })

  test('falls back to a neutral localized label without a recorded tier', () => {
    assert.equal(getUnresolvedTierText({}, t), '特殊计费表达式')
    assert.equal(
      getUnresolvedTierText({ matched_tier: '  ' }, t),
      '特殊计费表达式'
    )
    assert.equal(getUnresolvedTierText(null, t), '特殊计费表达式')
  })

  for (const [name, expr] of [
    ['missing expression', undefined],
    ['invalid encoded expression', '%'],
    [
      'tier absent from the parsed expression',
      b64('tier("different", p * 99 + c * 99)'),
    ],
  ] as const) {
    test(`${name} reaches the unresolved path without guessing prices`, () => {
      const other: LogOtherData = {
        billing_mode: 'tiered_expr',
        matched_tier: 'recorded',
        expr_b64: expr,
      }
      assert.equal(getTieredBillingSummary(other), null)
      assert.equal(getUnresolvedTierText(other, t), 'recorded')
    })
  }

  test('a parsed recorded tier still resolves to its prices', () => {
    const summary = getTieredBillingSummary({
      billing_mode: 'tiered_expr',
      matched_tier: 'standard',
      expr_b64: b64('tier("standard", p * 2 + c * 10)'),
    })
    assert.ok(summary)
    assert.equal(summary.tier.label, 'standard')
  })
})
