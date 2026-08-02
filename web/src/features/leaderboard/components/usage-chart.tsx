import { useEffect, useMemo, useRef, useState } from 'react'
import { VChart } from '@visactor/react-vchart'
import { VCHART_OPTION } from '@/lib/vchart'
import { useTheme } from '@/context/theme-provider'
import { bucketLabel, formatTokens } from '../lib'
import type { ModelRankingSeriesPoint } from '../types'

let themeManagerPromise: Promise<
  (typeof import('@visactor/vchart'))['ThemeManager']
> | null = null

interface UsageChartProps {
  series: ModelRankingSeriesPoint[]
  bucket: 'hour' | 'day'
}

export function UsageChart({ series, bucket }: UsageChartProps) {
  const { resolvedTheme } = useTheme()
  const [themeReady, setThemeReady] = useState(false)
  const themeManagerRef = useRef<
    (typeof import('@visactor/vchart'))['ThemeManager'] | null
  >(null)

  useEffect(() => {
    const updateTheme = async () => {
      setThemeReady(false)
      if (!themeManagerPromise) {
        themeManagerPromise = import('@visactor/vchart').then(
          (m) => m.ThemeManager
        )
      }
      const ThemeManager = await themeManagerPromise
      themeManagerRef.current = ThemeManager
      ThemeManager.setCurrentTheme(
        resolvedTheme === 'midnight' ? 'dark' : 'light'
      )
      setThemeReady(true)
    }
    updateTheme()
  }, [resolvedTheme])

  const values = useMemo(
    () =>
      series.map((p) => ({
        time: bucketLabel(p.time, bucket),
        model: p.model_name,
        token: p.token_used,
      })),
    [series, bucket]
  )

  const spec = useMemo(
    () => ({
      type: 'bar' as const,
      data: [{ id: 'usage', values }],
      xField: 'time',
      yField: 'token',
      seriesField: 'model',
      stack: true,
      legends: { visible: true, position: 'middle' as const, orient: 'bottom' as const },
      axes: [
        {
          orient: 'left' as const,
          label: { formatMethod: (v: number | string) => formatTokens(Number(v)) },
        },
        { orient: 'bottom' as const },
      ],
      tooltip: {
        dimension: {
          content: [
            {
              key: (d: Record<string, unknown>) => String(d.model),
              value: (d: Record<string, unknown>) => formatTokens(Number(d.token)),
            },
          ],
        },
      },
      theme: resolvedTheme === 'midnight' ? 'dark' : 'light',
      background: 'transparent',
    }),
    [values, resolvedTheme]
  )

  if (!themeReady || values.length === 0) {
    return (
      <div className='text-muted-foreground/60 flex h-full items-center justify-center text-sm'>
        {values.length === 0 ? '—' : ''}
      </div>
    )
  }

  return (
    <VChart
      key={resolvedTheme}
      spec={spec}
      option={VCHART_OPTION}
    />
  )
}
