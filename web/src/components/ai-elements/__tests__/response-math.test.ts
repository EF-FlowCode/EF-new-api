import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { isLikelyCurrencyMath } from '../response-math-utils'

describe('response math', () => {
  test('treats dollar amounts as text, not formulas', () => {
    // "costs $5 and $10" is parsed as `$5 and $`
    assert.ok(isLikelyCurrencyMath('5 and ', '$'))
    assert.ok(isLikelyCurrencyMath(' x', '$'))
  })

  test('keeps real formulas', () => {
    assert.ok(!isLikelyCurrencyMath('e^{i\\pi}+1=0', '$'))
    assert.ok(!isLikelyCurrencyMath(' a + b ', '\\(\\)'))
    assert.ok(!isLikelyCurrencyMath('x ', '$$'))
  })
})
