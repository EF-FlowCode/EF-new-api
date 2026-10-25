import assert from 'node:assert/strict'
import { test } from 'node:test'
import { TIME_RANGE_BY_GRANULARITY, TIME_RANGE_PRESETS } from './constants'

test('各时间粒度的默认范围都对应一个范围预设按钮', () => {
  const presetDays = new Set<number>(TIME_RANGE_PRESETS.map((p) => p.days))
  for (const [granularity, days] of Object.entries(TIME_RANGE_BY_GRANULARITY)) {
    assert.ok(
      presetDays.has(days),
      `${granularity} 的默认范围 ${days} 天不在预设 ${[...presetDays].join('/')} 中`
    )
  }
})
