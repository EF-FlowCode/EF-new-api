import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  isTokenBelowWarningLine,
  parseTokenQuotaWarningSettings,
  tokenQuotaWarningLine,
} from './quota-warning'

describe('token quota warning line', () => {
  test('defaults to five percent of the total', () => {
    assert.equal(tokenQuotaWarningLine({}, 1_000_000), 50_000)
  })

  test('the higher of percentage and amount wins', () => {
    assert.equal(
      tokenQuotaWarningLine({ token_quota_warning_amount: 80_000 }, 1_000_000),
      80_000
    )
    assert.equal(
      tokenQuotaWarningLine(
        { token_quota_warning_percent: 10, token_quota_warning_amount: 80_000 },
        1_000_000
      ),
      100_000
    )
  })

  test('zero turns a condition off; disabled or both off means no line', () => {
    assert.equal(
      tokenQuotaWarningLine(
        { token_quota_warning_percent: 0, token_quota_warning_amount: 30_000 },
        1_000_000
      ),
      30_000
    )
    assert.equal(
      tokenQuotaWarningLine({ token_quota_warning_percent: 0 }, 1_000_000),
      null
    )
    assert.equal(
      tokenQuotaWarningLine(
        { token_quota_warning_disabled: true, token_quota_warning_amount: 1 },
        1_000_000
      ),
      null
    )
  })

  test('below the line only when remaining is under it', () => {
    assert.equal(isTokenBelowWarningLine({}, 49_999, 950_001), true)
    assert.equal(isTokenBelowWarningLine({}, 50_000, 950_000), false)
  })

  test('parses the setting field as JSON or object', () => {
    assert.deepEqual(
      parseTokenQuotaWarningSettings('{"token_quota_warning_percent":20}'),
      { token_quota_warning_percent: 20 }
    )
    assert.deepEqual(parseTokenQuotaWarningSettings('not json'), {})
    assert.deepEqual(parseTokenQuotaWarningSettings(undefined), {})
  })
})
