import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatRelativeTimestamp, toIntlLocale } from './relative-time'

const now = Date.UTC(2026, 9, 16, 12, 0, 0)

test('相对时间跟随界面语言，不再固定输出英文', () => {
  const twoHoursAgo = now - 2 * 3600 * 1000
  assert.equal(formatRelativeTimestamp(twoHoursAgo, 'en', now), '2 hours ago')
  const zh = formatRelativeTimestamp(twoHoursAgo, toIntlLocale('zh'), now)
  assert.match(zh, /小时/)
  assert.doesNotMatch(zh, /ago/)
  const fr = formatRelativeTimestamp(twoHoursAgo, toIntlLocale('fr'), now)
  assert.match(fr, /heure/)
})

test('按量级选择单位', () => {
  assert.equal(
    formatRelativeTimestamp(now - 30 * 1000, 'en', now),
    '30 seconds ago'
  )
  assert.equal(
    formatRelativeTimestamp(now - 5 * 60 * 1000, 'en', now),
    '5 minutes ago'
  )
  assert.equal(
    formatRelativeTimestamp(now - 3 * 86400 * 1000, 'en', now),
    '3 days ago'
  )
  assert.equal(
    formatRelativeTimestamp(now - 400 * 86400 * 1000, 'en', now),
    '1 year ago'
  )
})

test('无效时间与未知语言码', () => {
  assert.equal(formatRelativeTimestamp(Number.NaN, 'en', now), '-')
  assert.equal(formatRelativeTimestamp(0, 'en', now), '-')
  assert.equal(toIntlLocale(''), undefined)
  assert.equal(toIntlLocale('not a locale!'), undefined)
})
