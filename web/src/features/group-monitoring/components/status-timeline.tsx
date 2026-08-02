import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import type { HistoryPoint } from '../types'

function segmentColor(rate: number | null | undefined): string {
  if (rate == null || rate < 0) return 'var(--muted)'
  if (rate >= 99) return 'rgb(34 197 94)' // green-500
  if (rate >= 95) return 'rgb(132 204 22)' // lime-500
  if (rate >= 80) return 'rgb(245 158 11)' // amber-500
  if (rate >= 50) return 'rgb(249 115 22)' // orange-500
  return 'rgb(239 68 68)' // red-500
}

function segmentLabel(
  rate: number | null | undefined,
  t: (k: string) => string
): string {
  if (rate == null || rate < 0) return t('No data')
  if (rate >= 99) return t('Healthy')
  if (rate >= 95) return t('Mild jitter')
  if (rate >= 80) return t('Partial issues')
  if (rate >= 50) return t('Severe issues')
  return t('Outage')
}

function formatTime(unixSec: number | undefined): string {
  if (!unixSec) return '-'
  return new Date(unixSec * 1000).toLocaleString([], {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

type Props = {
  history?: HistoryPoint[]
  segmentCount?: number
  compact?: boolean
}

/**
 * Right-aligned, fixed-segment status bar. Latest sample sits on the
 * right; older samples drift left. Empty slots render muted so the bar
 * keeps a stable visual width even when history is short.
 */
export function StatusTimeline({
  history,
  segmentCount = 60,
  compact = false,
}: Props) {
  const { t } = useTranslation()

  const segments = useMemo(() => {
    const sorted = (history || [])
      .filter((h) => h && typeof h.recorded_at === 'number')
      .sort((a, b) => a.recorded_at - b.recorded_at)
    const slice = sorted.slice(-segmentCount)
    const pad: Array<HistoryPoint | null> = Array(
      Math.max(0, segmentCount - slice.length)
    ).fill(null)
    return [...pad, ...slice] as Array<HistoryPoint | null>
  }, [history, segmentCount])

  const filled = segments.filter((s) => s != null).length
  const height = compact ? 22 : 32

  return (
    <div className='space-y-1.5'>
      {!compact && (
        <div className='text-muted-foreground flex items-center justify-between text-[10px] font-medium uppercase tracking-wider'>
          <span>
            {filled <= 1
              ? t('History (latest)')
              : t('History ({{n}})', { n: filled })}
          </span>
          <span className='opacity-60'>{t('Left → right: old → new')}</span>
        </div>
      )}
      <TooltipProvider delayDuration={150}>
        <div
          className='bg-muted/40 flex w-full overflow-hidden rounded-md'
          style={{ height, gap: 2, padding: 2 }}
        >
          {segments.map((seg, idx) => {
            const rate = seg?.availability_rate ?? null
            const bg = segmentColor(rate)
            const isEmpty = seg == null
            return (
              <Tooltip key={idx}>
                <TooltipTrigger asChild>
                  <div
                    className={cn(
                      'flex-1 rounded-sm transition-opacity hover:opacity-80'
                    )}
                    style={{
                      background: bg,
                      opacity: isEmpty ? 0.35 : 1,
                      minWidth: 2,
                    }}
                  />
                </TooltipTrigger>
                <TooltipContent>
                  {isEmpty ? (
                    <span className='text-xs'>{t('No data')}</span>
                  ) : (
                    <div className='space-y-0.5 text-xs'>
                      <div className='font-medium'>
                        {formatTime(seg?.recorded_at)}
                      </div>
                      <div>
                        {t('Status')}: {segmentLabel(rate, t)}
                      </div>
                      {rate != null && rate >= 0 && (
                        <div>
                          {t('Availability')}: {rate.toFixed(1)}%
                        </div>
                      )}
                      {seg?.cache_hit_rate != null && seg.cache_hit_rate >= 0 && (
                        <div>
                          {t('Cache hit')}: {seg.cache_hit_rate.toFixed(1)}%
                        </div>
                      )}
                    </div>
                  )}
                </TooltipContent>
              </Tooltip>
            )
          })}
        </div>
      </TooltipProvider>
    </div>
  )
}
