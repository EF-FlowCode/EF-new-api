import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  combineBillingExpr,
  parseFlatTokenBillingExpr,
  splitBillingExprAndRequestRules,
} from './billing-expr'
import {
  expressionAmount,
  legacyExpressionView,
  toAmountExpression,
} from './expression-units'
import {
  buildFlatTokenPriceExpr,
  EMPTY_FLAT_TOKEN_PRICES,
  parseFlatTokenPriceExpr,
} from './flat-token-price'
import {
  evalExprLocally,
  generateExprFromVisualConfig,
  tryParseVisualConfig,
} from './tier-expr'

const extras = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
}

describe('amount expressions', () => {
  test('fixed prices retain their per-million display and charge actual amounts', () => {
    const expression = buildFlatTokenPriceExpr({
      ...EMPTY_FLAT_TOKEN_PRICES,
      inputPrice: '2',
      outputPrice: '10',
      cacheReadPrice: '0',
    })!
    assert.ok(expression.startsWith('v2:'))
    assert.equal(parseFlatTokenPriceExpr(expression)?.inputPrice, '2')
    assert.equal(parseFlatTokenPriceExpr(expression)?.cacheReadPrice, '0')
    assert.equal(evalExprLocally(expression, 1000, 100, extras).cost, 0.003)
    assert.equal(
      parseFlatTokenBillingExpr('v2:tier("base", p * 0.000002 + c * 0.00001)')
        ?.coefficients.p,
      2
    )
  })

  test('migration is idempotent and preserves the whole calculation', () => {
    const legacy =
      'len <= 1000 ? tier("short", p * 2 + c * 10) : tier("long", p * 4 + c * 20)'
    const amount = toAmountExpression(legacy)
    assert.equal(toAmountExpression(amount), amount)
    assert.equal(legacyExpressionView(amount), legacy)
    for (const tokens of [0, 1, 999, 1000, 1001, 1000000]) {
      assert.deepEqual(
        evalExprLocally(amount, tokens, 50, extras),
        evalExprLocally(legacy, tokens, 50, extras)
      )
    }
    const visual = tryParseVisualConfig(amount)
    assert.ok(visual)
    assert.equal(
      evalExprLocally(generateExprFromVisualConfig(visual), 1001, 50, extras)
        .cost,
      0.005004
    )
  })

  test('request rules stay outside the unit conversion and survive edits', () => {
    const base = toAmountExpression('tier("base", p * 2 + c * 10)')
    const rules =
      '(hour("Asia/Shanghai") >= 8 || hour("Asia/Shanghai") < 22 ? 2 : 1)'
    const combined = combineBillingExpr(base, rules)
    const split = splitBillingExprAndRequestRules(combined)
    assert.equal(split.requestRuleExpr, rules)
    assert.equal(
      legacyExpressionView(split.billingExpr),
      legacyExpressionView(base)
    )
    assert.equal(
      combineBillingExpr(split.billingExpr, split.requestRuleExpr),
      combined
    )
  })

  test('legacy version prefixes do not become nested syntax when adding rules', () => {
    const base = 'v1:tier("base", p * 2 + c * 10)'
    const rules = '(hour("UTC") >= 9 ? 2 : 1)'
    const combined = combineBillingExpr(base, rules)
    assert.equal(combined, `v1:(tier("base", p * 2 + c * 10)) * ${rules}`)
    assert.deepEqual(splitBillingExprAndRequestRules(combined), {
      billingExpr: base,
      requestRuleExpr: rules,
    })
    assert.ok(toAmountExpression(combined).startsWith('v2:('))
  })

  test('accepts every 1e6 divisor form the backend treats as a fixed price', () => {
    const tier = 'tier("base", p * 2 + c * 10)'
    for (const expression of [
      `v2:${tier} / 1000000`,
      `v2:${tier} / 1e6`,
      `v2:${tier}/1E+6`,
      `v2:${tier} / 1000000.0`,
      `v2:${tier} / 1_000_000`,
      `v2:(${tier}) / 1e6`,
      `v2:(${tier} / 1e6)`,
      `v2:${tier} / 1e6 // per million`,
      `v2:(${tier.replace('p * 2', 'p * 0.000002').replace('c * 10', 'c * 0.00001')})`,
    ]) {
      assert.deepEqual(
        parseFlatTokenBillingExpr(expression)?.coefficients,
        { p: 2, c: 10 },
        expression
      )
      assert.equal(parseFlatTokenPriceExpr(expression)?.inputPrice, '2')
    }
    assert.equal(legacyExpressionView(`v2:${tier} / 1e6`), tier)
    assert.equal(legacyExpressionView(`v2:(${tier}) / 1000000.0`), tier)
    for (const expression of [
      `v2:${tier} / 1000`,
      `v2:${tier} / 1e6 + 0`,
      `v2:${tier} / 1e6 / 2`,
      `v2:${tier} * 1e6`,
      `v2:len > 1 ? ${tier} : ${tier} / 1e6`,
    ]) {
      assert.equal(legacyExpressionView(expression), null, expression)
      assert.equal(parseFlatTokenBillingExpr(expression), null, expression)
    }
  })

  test('does not misread arbitrary arithmetic, quoted parentheses or comments', () => {
    const legacy = 'tier("a)\\"", p * 2 + c * 10) // )'
    assert.equal(legacyExpressionView(toAmountExpression(legacy)), legacy)
    assert.equal(legacyExpressionView('v2:(p) / 1000'), null)
    assert.equal(legacyExpressionView('v2:(p) / 1000000 + 1'), null)
    assert.equal(
      parseFlatTokenBillingExpr('v3:tier("base", p * 2 + c * 10)'),
      null
    )
    assert.throws(() => expressionAmount(1, 3))
    assert.ok(evalExprLocally('v2:1/0', 0, 0, extras).error)
    assert.ok(evalExprLocally('v2:u("seconds") * 0.01', 0, 0, extras).error)
  })
})
