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
