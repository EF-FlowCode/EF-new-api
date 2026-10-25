import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  MATCH_EQ,
  MATCH_GTE,
  MATCH_RANGE,
  buildRequestRuleExpr,
  hasLegacyAlwaysTrueTimeRange,
  hasNonCanonicalTimeRange,
  tryParseRequestRuleExpr,
  type RequestRuleGroup,
  type TimeCondition,
  type TimeFunc,
} from './billing-expr'

function timeRangeGroup(start: string, end: string): RequestRuleGroup {
  return {
    conditions: [
      {
        source: 'time',
        timeFunc: 'hour',
        timezone: 'Asia/Shanghai',
        mode: MATCH_RANGE,
        value: '',
        rangeStart: start,
        rangeEnd: end,
      },
    ],
    multiplier: '2',
  }
}

describe('time range expression generation', () => {
  test('uses AND for a range within the same day', () => {
    assert.equal(
      buildRequestRuleExpr([timeRangeGroup('9', '12')]),
      '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)'
    )
  })

  test('uses OR only for a range crossing midnight', () => {
    assert.equal(
      buildRequestRuleExpr([timeRangeGroup('21', '6')]),
      '(hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)'
    )
  })

  test('uses an always-false AND expression for equal bounds', () => {
    assert.equal(
      buildRequestRuleExpr([timeRangeGroup('9', '9')]),
      '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 9 ? 2 : 1)'
    )
  })

  test('drops out-of-domain and fractional hour ranges', () => {
    assert.equal(buildRequestRuleExpr([timeRangeGroup('-1', '5')]), '')
    assert.equal(buildRequestRuleExpr([timeRangeGroup('9', '24')]), '')
    assert.equal(buildRequestRuleExpr([timeRangeGroup('9.5', '12')]), '')
  })
})

describe('time range expression parsing', () => {
  test('parses both range forms without losing their original operator', () => {
    for (const [expression, operator] of [
      [
        '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)',
        '&&',
      ],
      [
        '(hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)',
        '||',
      ],
    ] as const) {
      const groups = tryParseRequestRuleExpr(expression)
      assert.equal(groups?.[0].conditions.length, 1)
      assert.equal(groups?.[0].conditions[0].mode, MATCH_RANGE)
      assert.equal(
        (groups?.[0].conditions[0] as TimeCondition).rangeOperator,
        operator
      )
    }
  })

  test('rejects out-of-domain stored ranges instead of normalizing them', () => {
    assert.equal(
      tryParseRequestRuleExpr(
        '(hour("Asia/Shanghai") >= 25 && hour("Asia/Shanghai") < 30 ? 2 : 1)'
      ),
      null
    )
  })
})

describe('hasLegacyAlwaysTrueTimeRange', () => {
  test('detects a same-day range stored with OR', () => {
    assert.equal(
      hasLegacyAlwaysTrueTimeRange(
        '(hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12 ? 2 : 1)'
      ),
      true
    )
  })

  test('does not flag overnight OR or correct same-day AND ranges', () => {
    assert.equal(
      hasLegacyAlwaysTrueTimeRange(
        '(hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)'
      ),
      false
    )
    assert.equal(
      hasLegacyAlwaysTrueTimeRange(
        '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)'
      ),
      false
    )
  })
})

describe('hasNonCanonicalTimeRange', () => {
  test('detects both always-true and always-false range combinations', () => {
    assert.equal(
      hasNonCanonicalTimeRange(
        '(hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12 ? 2 : 1)'
      ),
      true
    )
    assert.equal(
      hasNonCanonicalTimeRange(
        '(hour("Asia/Shanghai") >= 21 && hour("Asia/Shanghai") < 6 ? 2 : 1)'
      ),
      true
    )
  })

  test('accepts canonical within-day, overnight, and zero-length ranges', () => {
    for (const expression of [
      '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)',
      '(hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)',
      '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 9 ? 2 : 1)',
    ]) {
      assert.equal(hasNonCanonicalTimeRange(expression), false)
    }
  })
})

test('parsed range keeps its exact bounds', () => {
  const groups = tryParseRequestRuleExpr(
    '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)'
  )
  const condition = groups?.[0].conditions[0] as TimeCondition
  assert.equal(condition.rangeStart, '9')
  assert.equal(condition.rangeEnd, '12')
})

describe('stored contracts and mixed rules', () => {
  for (const expression of [
    '(hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12 ? 2 : 1)',
    '(hour("Asia/Shanghai") >= 21 && hour("Asia/Shanghai") < 6 ? 2 : 1)',
    '(param("service_tier") == "fast" && hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)',
    '((hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6) && param("service_tier") == "fast" ? 3 : 1)',
    '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1) * (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18 ? 2 : 1)',
  ]) {
    test(`round-trips without rewriting: ${expression}`, () => {
      const groups = tryParseRequestRuleExpr(expression)
      assert.ok(groups)
      assert.equal(buildRequestRuleExpr(groups), expression)
    })
  }

  test('merges adjacent bounds into one condition beside a parameter', () => {
    const groups = tryParseRequestRuleExpr(
      '(param("service_tier") == "fast" && hour("UTC") >= 9 && hour("UTC") < 12 ? 2 : 1)'
    )
    assert.deepEqual(
      groups?.[0].conditions.map((c) => c.mode),
      [MATCH_EQ, MATCH_RANGE]
    )
  })

  test('changing a multiplier preserves the stored range operator', () => {
    const groups = tryParseRequestRuleExpr(
      '(hour("UTC") >= 9 || hour("UTC") < 12 ? 2 : 1)'
    )
    assert.ok(groups)
    groups[0].multiplier = '3'
    assert.equal(
      buildRequestRuleExpr(groups),
      '(hour("UTC") >= 9 || hour("UTC") < 12 ? 3 : 1)'
    )
  })
})

describe('all time function domains', () => {
  const domains: [TimeFunc, number, number][] = [
    ['hour', 0, 23],
    ['minute', 0, 59],
    ['weekday', 0, 6],
    ['month', 1, 12],
    ['day', 1, 31],
  ]
  for (const [timeFunc, min, max] of domains) {
    test(`${timeFunc}: scalar and range boundaries, fractions and invalid values`, () => {
      for (const value of [min, max, min - 1, max + 1, min + 0.5]) {
        const valid = value === min || value === max
        const scalar: TimeCondition = {
          source: 'time',
          timeFunc,
          timezone: 'UTC',
          mode: MATCH_GTE,
          value: String(value),
          rangeStart: '',
          rangeEnd: '',
        }
        const range: TimeCondition = {
          ...scalar,
          mode: MATCH_RANGE,
          value: '',
          rangeStart: String(min),
          rangeEnd: String(value),
        }
        for (const condition of [scalar, range]) {
          assert.equal(
            buildRequestRuleExpr([
              { conditions: [condition], multiplier: '2' },
            ]) !== '',
            valid
          )
        }
        for (const expression of [
          `(${timeFunc}("UTC") >= ${value} ? 2 : 1)`,
          `(${timeFunc}("UTC") >= ${min} && ${timeFunc}("UTC") < ${value} ? 2 : 1)`,
        ]) {
          assert.equal(tryParseRequestRuleExpr(expression) !== null, valid)
        }
      }
    })
  }
})
