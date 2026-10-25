import type { ModelPerformancePoint, MonitoringModelsResult } from '../types'

export function performanceColor(rate: number | null | undefined): string {
  if (rate == null || !Number.isFinite(rate) || rate < 0) return 'var(--muted)'
  if (rate >= 99) return 'var(--monitoring-healthy)'
  if (rate >= 95) return 'var(--monitoring-mild)'
  if (rate >= 80) return 'var(--monitoring-warning)'
  if (rate >= 50) return 'var(--monitoring-severe)'
  return 'var(--monitoring-outage)'
}

export function formatMetricTime(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return '—'
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatMetricTps(tps: number): string {
  return Number.isFinite(tps) && tps > 0 ? `${tps.toFixed(1)} t/s` : '—'
}

// Preserve holes and the common server window; never slide old data to "now".
export function modelTimelineSlots(
  series: ModelPerformancePoint[],
  window: Pick<
    MonitoringModelsResult,
    'start_time' | 'end_time' | 'bucket_seconds'
  >
) {
  const { start_time: start, end_time: end, bucket_seconds: step } = window
  if (
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    !Number.isFinite(step) ||
    step <= 0 ||
    end < start
  )
    return []
  const first = Math.floor(start / step) * step
  const count = Math.floor((end - first) / step) + 1
  if (count > 100) return []
  const points = new Map(series.map((point) => [point.ts, point]))
  return Array.from({ length: count }, (_, index) => {
    const ts = first + index * step
    return { ts, end: Math.min(ts + step, end), point: points.get(ts) }
  })
}
