import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  modelTimelineSlots,
  formatMetricTime,
  formatMetricTps,
} from './model-performance.ts'

test('model timeline preserves empty hours and does not move stale samples to now', () => {
  const point = {
    ts: 3600,
    avg_ttft_ms: 200,
    avg_latency_ms: 1000,
    avg_tps: 30,
    success_rate: 0,
  }
  const slots = modelTimelineSlots([point], {
    start_time: 1800,
    end_time: 12600,
    bucket_seconds: 3600,
  })
  assert.deepEqual(
    slots.map((slot) => slot.ts),
    [0, 3600, 7200, 10800]
  )
  assert.equal(slots[0].point, undefined)
  assert.equal(slots[1].point?.success_rate, 0)
  assert.equal(slots[2].point, undefined)
  assert.equal(slots[3].point, undefined)
  assert.equal(slots[3].end, 12600)
})

test('missing performance values stay distinct from measured durations', () => {
  assert.equal(formatMetricTime(0), '—')
  assert.equal(formatMetricTime(null), '—')
  assert.equal(formatMetricTime(7720), '7.72s')
  assert.equal(formatMetricTps(0), '—')
  assert.equal(formatMetricTps(42.56), '42.6 t/s')
})
