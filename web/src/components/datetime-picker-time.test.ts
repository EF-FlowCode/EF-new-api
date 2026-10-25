import assert from 'node:assert/strict'
import { test } from 'node:test'
import { applyTimeOfDay, formatTimeOfDay } from './datetime-picker-time'

const base = new Date(2026, 9, 16, 8, 30, 45, 123)

test('时间显示与回写都保留秒', () => {
  assert.equal(formatTimeOfDay(base), '08:30:45')
  const next = applyTimeOfDay(base, '23:59:58')
  assert.ok(next)
  assert.equal(formatTimeOfDay(next), '23:59:58')
  assert.equal(next.getMilliseconds(), 0)
  assert.equal(next.getDate(), 16)
  // 原对象不被修改
  assert.equal(formatTimeOfDay(base), '08:30:45')
})

test('浏览器省略 :00 秒时按 0 秒处理', () => {
  const next = applyTimeOfDay(base, '09:15')
  assert.ok(next)
  assert.equal(formatTimeOfDay(next), '09:15:00')
})

test('清空或半清空的时间不产生非法日期', () => {
  assert.equal(applyTimeOfDay(base, ''), null)
  assert.equal(applyTimeOfDay(base, '12:'), null)
  assert.equal(applyTimeOfDay(base, '25:00:00'), null)
  assert.equal(applyTimeOfDay(base, 'ab:cd'), null)
})
