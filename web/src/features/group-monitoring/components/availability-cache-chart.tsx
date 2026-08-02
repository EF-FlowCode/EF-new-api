import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { VChart } from '@visactor/react-vchart'
import { VCHART_OPTION } from '@/lib/vchart'
import { useTheme } from '@/context/theme-provider'
import type { HistoryPoint } from '../types'

type ChartPoint = {
  time: string
  value: number
  type: 'availability' | 'cache'
}

function alignAndFillHistory(
  history: HistoryPoint[] | undefined,
  intervalMinutes: number
): ChartPoint[] {
  if (!history || history.length === 0) return []

  const sorted = [...history].sort(
    (a, b) => a.recorded_at * 1000 - b.recorded_at * 1000
  )
  const startMs = sorted[0].recorded_at * 1000
  const endMs = sorted[sorted.length - 1].recorded_at * 1000
  const stepMs = (intervalMinutes || 5) * 60 * 1000

  const byTime: Record<number, HistoryPoint> = {}
  for (const h of sorted) {
    const t = h.recorded_at * 1000
    const aligned = Math.round(t / stepMs) * stepMs
    byTime[aligned] = h
  }

  const result: ChartPoint[] = []
  let lastAvail: number | null = null
  let lastCache: number | null = null

  for (let t = startMs; t <= endMs; t += stepMs) {
    const aligned = Math.round(t / stepMs) * stepMs
    const entry = byTime[aligned]
    if (entry) {
      if (entry.availability_rate != null && entry.availability_rate >= 0) {
        lastAvail = entry.availability_rate
      }
      if (entry.cache_hit_rate != null && entry.cache_hit_rate >= 0) {
        lastCache = entry.cache_hit_rate
      }
    }
    const timeStr = new Date(aligned).toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    })
    if (lastAvail !== null) {
      result.push({ time: timeStr, value: lastAvail, type: 'availability' })
    }
    if (lastCache !== null) {
      result.push({ time: timeStr, value: lastCache, type: 'cache' })
    }
  }

  return result
}

type Props = {
  history: HistoryPoint[]
  intervalMinutes: number
  compact?: boolean
}

export function AvailabilityCacheChart({
  history,
  intervalMinutes,
  compact,
}: Props) {
  const { t } = useTranslation()
  const { resolvedTheme } = useTheme()
  const [themeReady, setThemeReady] = useState(false)

  useEffect(() => {
    let active = true
    import('@visactor/vchart').then((mod) => {
      if (!active) return
      try {
        mod.ThemeManager.setCurrentTheme(
          resolvedTheme === 'midnight' ? 'dark' : 'light'
        )
      } catch {
        // ignore
      }
      setThemeReady(true)
    })
    return () => {
      active = false
    }
  }, [resolvedTheme])

  const chartData = useMemo(
    () => alignAndFillHistory(history, intervalMinutes),
    [history, intervalMinutes]
  )

  const yMin = useMemo(() => {
    if (!chartData.length) return 0
    const vals = chartData.map((d) => d.value).filter((v) => v > 0)
    if (vals.length === 0) return 0
    const min = Math.min(...vals)
    return Math.max(0, Math.floor(min / 5) * 5 - 5)
  }, [chartData])

  const h = compact ? 120 : 260

  if (chartData.length === 0) {
    return (
      <div
        style={{ height: h }}
        className='text-muted-foreground flex items-center justify-center text-sm'
      >
        {t('No history yet')}
      </div>
    )
  }

  const spec = {
    type: 'line' as const,
    data: [{ id: 'history', values: chartData }],
    xField: 'time',
    yField: 'value',
    seriesField: 'type',
    height: h,
    padding: compact
      ? { top: 4, bottom: 20, left: 4, right: 4 }
      : { top: 12, bottom: 24, left: 8, right: 8 },
    animation: !compact,
    line: {
      style: {
        lineWidth: compact ? 1.5 : 2,
        curveType: 'monotone',
      },
    },
    point: { visible: false },
    axes: [
      {
        orient: 'bottom',
        label: {
          autoRotate: true,
          autoHide: true,
          style: { fontSize: compact ? 9 : 11 },
        },
      },
      {
        orient: 'left',
        min: yMin,
        max: 100,
        label: compact
          ? { visible: false }
          : {
              formatMethod: (v: number) => `${v}%`,
              style: { fontSize: 11 },
            },
      },
    ],
    legends: compact
      ? { visible: false }
      : {
          visible: true,
          orient: 'top',
          position: 'start',
          data: [
            { label: t('Availability'), shape: { fill: '#3b82f6' } },
            { label: t('Cache hit'), shape: { fill: '#22c55e' } },
          ],
        },
    tooltip: {
      mark: {
        content: [
          {
            key: (datum: ChartPoint) =>
              datum.type === 'availability' ? t('Availability') : t('Cache hit'),
            value: (datum: ChartPoint) => `${datum.value.toFixed(1)}%`,
          },
        ],
      },
      dimension: {
        content: [
          {
            key: (datum: ChartPoint) =>
              datum.type === 'availability' ? t('Availability') : t('Cache hit'),
            value: (datum: ChartPoint) => `${datum.value.toFixed(1)}%`,
          },
        ],
      },
    },
    color: ['#3b82f6', '#22c55e'],
    crosshair: {
      xField: { visible: true, line: { type: 'line' } },
    },
  }

  if (!themeReady) {
    return <div style={{ height: h }} />
  }

  return <VChart spec={spec} option={VCHART_OPTION} />
}
