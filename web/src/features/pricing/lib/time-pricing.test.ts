import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getTimePricingSummary } from './time-pricing'

const expression =
  '(tier("base", p * 0.375 + c * 0.675 + cr * 0.0075)) * (hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1) * (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18 ? 2 : 1)'

describe('getTimePricingSummary', () => {
  test('extracts daytime windows and evaluates the active multiplier', () => {
    const morning = getTimePricingSummary(
      expression,
      new Date('2026-08-17T02:00:00Z')
    )
    const lunch = getTimePricingSummary(
      expression,
      new Date('2026-08-17T05:00:00Z')
    )
    const afternoon = getTimePricingSummary(
      expression,
      new Date('2026-08-17T07:00:00Z')
    )

    assert.ok(morning)
    assert.deepEqual(morning.periods, [
      {
        timezone: 'Asia/Shanghai',
        startHour: 9,
        endHour: 12,
        multiplier: 2,
      },
      {
        timezone: 'Asia/Shanghai',
        startHour: 14,
        endHour: 18,
        multiplier: 2,
      },
    ])
    assert.equal(morning.currentMultiplier, 2)
    assert.equal(lunch?.currentMultiplier, 1)
    assert.equal(afternoon?.currentMultiplier, 2)
  })

  test('keeps the original OR semantics for overnight ranges', () => {
    const overnight = getTimePricingSummary(
      'tier("base", p * 1) * (hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 0.5 : 1)',
      new Date('2026-08-17T15:00:00Z')
    )

    assert.ok(overnight)
    assert.equal(overnight.currentMultiplier, 0.5)
    assert.deepEqual(overnight.periods[0], {
      timezone: 'Asia/Shanghai',
      startHour: 21,
      endHour: 6,
      multiplier: 0.5,
    })
  })

  test('treats equal range bounds as an inactive zero-length window', () => {
    const equalBounds = getTimePricingSummary(
      'tier("base", p * 1) * (hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 9 ? 2 : 1)',
      new Date('2026-08-17T01:00:00Z')
    )

    assert.ok(equalBounds)
    assert.equal(equalBounds.currentMultiplier, 1)
  })

  test('does not publish a misleading summary for a legacy always-true range', () => {
    const legacy = getTimePricingSummary(
      'tier("base", p * 1) * (hour("Asia/Shanghai") >= 9 || hour("Asia/Shanghai") < 12 ? 2 : 1)'
    )

    assert.equal(legacy, null)
  })

  test('does not publish a misleading summary for an always-false overnight AND range', () => {
    const nonCanonical = getTimePricingSummary(
      'tier("base", p * 1) * (hour("Asia/Shanghai") >= 21 && hour("Asia/Shanghai") < 6 ? 2 : 1)'
    )

    assert.equal(nonCanonical, null)
  })
})
