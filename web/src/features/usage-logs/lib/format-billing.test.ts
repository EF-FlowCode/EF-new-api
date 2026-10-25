import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { UsageLog } from '../data/schema'
import type { LogOtherData } from '../types'
import { buildBillingFormula } from './format'

const screenshotLog: UsageLog = {
  id: 5924801,
  user_id: 0,
  created_at: 0,
  type: 2,
  content: '',
  username: '',
  token_name: '',
  model_name: 'kimi-k2.6',
  quota: 7227,
  prompt_tokens: 3754,
  completion_tokens: 1970,
  use_time: 300,
  is_stream: false,
  channel: 151,
  channel_name: '',
  token_id: 0,
  group: '1_vip_china',
  ip: '',
  other: '',
  request_id: '',
}

const screenshotOther: LogOtherData = {
  model_ratio: 3.25,
  completion_ratio: 4.153846153846154,
  cache_tokens: 1024,
  cache_ratio: 0.2,
  group_ratio: 0.2,
  image: true,
  image_output: 2706,
  image_ratio: 1,
  model_price: -1,
  user_group_ratio: -1,
}

describe('billing formula image token normalization', () => {
  test('provides the complete data contract for readable billing process rows', () => {
    const formula = buildBillingFormula(screenshotLog, screenshotOther)

    assert.ok(formula)
    assert.deepEqual(
      formula.terms.map((term) => ({
        varName: term.varName,
        varLabel: term.varLabel,
        tokens: term.tokens,
      })),
      [
        {
          varName: 'p',
          varLabel: 'Input',
          tokens: 2730,
        },
        {
          varName: 'c',
          varLabel: 'Output',
          tokens: 1970,
        },
        {
          varName: 'cr',
          varLabel: 'Cache Read',
          tokens: 1024,
        },
      ]
    )
    const expectedAmounts = [
      { pricePerMillionUSD: 6.5, productUSD: 0.017745 },
      { pricePerMillionUSD: 27, productUSD: 0.05319 },
      { pricePerMillionUSD: 1.3, productUSD: 0.0013312 },
    ]
    formula.terms.forEach((term, index) => {
      const expected = expectedAmounts[index]
      assert.ok(
        Math.abs(term.pricePerMillionUSD - expected.pricePerMillionUSD) < 1e-12
      )
      assert.ok(Math.abs(term.productUSD - expected.productUSD) < 1e-12)
    })
    assert.equal(formula.groupRatio, 0.2)
    assert.ok(Math.abs(formula.subtotalUSD - 0.0722662) < 1e-12)
    assert.ok(Math.abs(formula.totalUSD - 0.01445324) < 1e-12)
  })

  test('explains how raw input tokens become normalized p tokens', () => {
    const formula = buildBillingFormula(
      {
        ...screenshotLog,
        quota: 15911,
        prompt_tokens: 7276,
        completion_tokens: 4314,
      },
      {
        ...screenshotOther,
        cache_tokens: 896,
      }
    )

    assert.ok(formula)
    assert.deepEqual(formula.tokenNormalizations, [
      {
        varName: 'p',
        rawTokens: 7276,
        deductions: [{ varName: 'cr', tokens: 896 }],
        normalizedTokens: 6380,
      },
    ])
  })

  test('keeps completion tokens when a legacy image field is not separately priced', () => {
    const formula = buildBillingFormula(screenshotLog, screenshotOther)

    assert.ok(formula)
    assert.deepEqual(
      formula.terms.map((term) => [term.varName, term.tokens]),
      [
        ['p', 2730],
        ['c', 1970],
        ['cr', 1024],
      ]
    )
    assert.ok(Math.abs(formula.totalUSD - 0.01445324) < 1e-12)
  })

  test('prices legacy image input tokens as img when the ratio is separate', () => {
    const formula = buildBillingFormula(screenshotLog, {
      ...screenshotOther,
      image_ratio: 2,
    })

    assert.ok(formula)
    assert.deepEqual(
      formula.terms.map((term) => [term.varName, term.tokens]),
      [
        ['p', 24],
        ['c', 1970],
        ['cr', 1024],
        ['img', 2706],
      ]
    )
  })

  test('uses explicit image input without removing output-side details from standard completion', () => {
    const formula = buildBillingFormula(screenshotLog, {
      ...screenshotOther,
      image_input: 2706,
      image_output: 100,
      image_ratio: 2,
    })

    assert.ok(formula)
    assert.deepEqual(
      formula.terms.map((term) => [term.varName, term.tokens]),
      [
        ['p', 24],
        ['c', 1970],
        ['cr', 1024],
        ['img', 2706],
      ]
    )
  })

  test('removes image output from c only when img_o is explicitly priced', () => {
    const expr = 'tier("base", p * 6.5 + c * 27 + img_o * 10)'
    const formula = buildBillingFormula(screenshotLog, {
      billing_mode: 'tiered_expr',
      expr_b64: Buffer.from(expr).toString('base64'),
      matched_tier: 'base',
      group_ratio: 0.2,
      image: true,
      image_input: 0,
      image_output: 100,
    })

    assert.ok(formula)
    assert.deepEqual(
      formula.terms.map((term) => [term.varName, term.tokens]),
      [
        ['p', 3754],
        ['c', 1870],
        ['img_o', 100],
      ]
    )
  })
})

describe('billing formula fixed token prices', () => {
  test('uses the recorded flat-price expression instead of zero ratios', () => {
    const formula = buildBillingFormula(
      {
        ...screenshotLog,
        prompt_tokens: 56,
        completion_tokens: 26,
      },
      {
        billing_mode: 'flat_price',
        expr_b64: Buffer.from(
          'tier("default", p * 0.8 + c * 2.7 + cr * 0.1)'
        ).toString('base64'),
        matched_tier: 'default',
        model_ratio: 0,
        completion_ratio: 0,
        group_ratio: 0.2,
        cache_tokens: 0,
      }
    )

    assert.ok(formula)
    assert.deepEqual(
      formula.terms.map((term) => [
        term.varName,
        term.tokens,
        term.pricePerMillionUSD,
      ]),
      [
        ['p', 56, 0.8],
        ['c', 26, 2.7],
      ]
    )
    assert.equal(formula.groupRatio, 0.2)
    assert.ok(Math.abs(formula.totalUSD - 0.000023) < 1e-12)
  })
})

describe('billing formula time-based request rules', () => {
  // Real log 2026101109114818777638695a3f475UIS6cMmb (09:11 Asia/Shanghai).
  const timedExpr =
    'v2:((tier("base", p * 1 + c * 4 + cr * 0.1)) * (hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 22 ? 2 : 1)\n) / 1000000'
  const timedOther: LogOtherData = {
    billing_mode: 'tiered_expr',
    expr_b64: Buffer.from(timedExpr).toString('base64'),
    matched_tier: 'base',
    claude: true,
    cache_tokens: 64384,
    group_ratio: 0.2,
    user_group_ratio: -1,
  }
  const timedLog: UsageLog = {
    ...screenshotLog,
    created_at: 1791709913,
    prompt_tokens: 64902,
    completion_tokens: 314,
    quota: 14519,
  }

  test('applies the time multiplier active when the request was logged', () => {
    const formula = buildBillingFormula(timedLog, timedOther)

    assert.ok(formula)
    assert.equal(formula.timeMultiplier, 2)
    assert.ok(Math.abs(formula.subtotalUSD - 0.0725964) < 1e-12)
    assert.ok(Math.abs(formula.totalUSD - 0.02903856) < 1e-12)
  })

  test('omits the time multiplier outside the configured hours', () => {
    const formula = buildBillingFormula(
      { ...timedLog, created_at: 1791709913 + 14 * 3600 },
      timedOther
    )

    assert.ok(formula)
    assert.equal(formula.timeMultiplier, null)
    assert.ok(Math.abs(formula.totalUSD - 0.01451928) < 1e-12)
  })
})
