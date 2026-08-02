import { useTranslation } from 'react-i18next'
import { Database, Zap } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { GroupSummary } from '../types'
import { StatusTimeline } from './status-timeline'

function rateColor(rate: number | null | undefined): string {
  if (rate == null || rate < 0) return 'var(--muted-foreground)'
  if (rate >= 99) return 'rgb(34 197 94)'
  if (rate >= 95) return 'rgb(132 204 22)'
  if (rate >= 80) return 'rgb(245 158 11)'
  return 'rgb(239 68 68)'
}

function formatFRT(ms: number | null | undefined): string {
  if (ms == null || ms <= 0) return '—'
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}

function formatClock(unixSec: number | undefined): string {
  if (!unixSec || unixSec <= 0) return ''
  return new Date(unixSec * 1000).toLocaleTimeString([], {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

type Props = {
  group: GroupSummary
  onClick?: (group: GroupSummary) => void
}

export function GroupStatusCard({ group, onClick }: Props) {
  const { t } = useTranslation()

  const isOnline = group.is_online ?? (group.online_channels ?? 0) > 0
  const availRate =
    group.availability_rate != null && group.availability_rate >= 0
      ? group.availability_rate
      : null
  const cacheRate =
    group.cache_hit_rate != null && group.cache_hit_rate >= 0
      ? group.cache_hit_rate
      : null
  const showCache = cacheRate != null && cacheRate >= 3
  const frt = group.avg_frt ?? group.first_response_time ?? null

  const dotColor = !isOnline
    ? 'rgb(239 68 68)'
    : availRate == null
      ? 'var(--muted)'
      : rateColor(availRate)

  const headlineColor = !isOnline ? 'rgb(239 68 68)' : rateColor(availRate)

  const interactive = typeof onClick === 'function'

  return (
    <button
      type='button'
      onClick={() => onClick?.(group)}
      disabled={!interactive}
      className={cn(
        'bg-card group relative w-full rounded-2xl border p-5 text-left transition-all duration-200',
        interactive
          ? 'hover:border-primary/40 hover:-translate-y-0.5 hover:shadow-[0_8px_24px_-12px_rgba(0,0,0,0.12)] cursor-pointer'
          : 'cursor-default'
      )}
    >
      {/* Header: name + meta on left, big availability on right */}
      <div className='flex items-start justify-between gap-3'>
        <div className='min-w-0 flex-1'>
          <div className='flex items-center gap-2'>
            <span
              className={cn(
                'inline-block h-2 w-2 flex-shrink-0 rounded-full',
                !isOnline && 'animate-pulse'
              )}
              style={{ background: dotColor }}
              aria-hidden
            />
            <span
              className='block truncate text-sm font-semibold'
              title={group.group_name}
            >
              {group.group_name}
            </span>
          </div>
          <div className='text-muted-foreground mt-1.5 flex items-center gap-1.5 text-[11px]'>
            {group.last_test_model && (
              <TooltipProvider delayDuration={300}>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span className='max-w-[140px] truncate font-mono'>
                      {group.last_test_model}
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{group.last_test_model}</TooltipContent>
                </Tooltip>
              </TooltipProvider>
            )}
            {group.last_test_model && group.group_ratio != null && (
              <span className='opacity-60'>·</span>
            )}
            {group.group_ratio != null && (
              <span>
                {group.group_ratio}x
              </span>
            )}
          </div>
        </div>

        <div className='flex-shrink-0 text-right leading-none'>
          {availRate != null ? (
            <div
              className='font-mono text-[28px] font-semibold tracking-tight'
              style={{ color: headlineColor }}
            >
              {availRate.toFixed(1)}
              <span className='ml-0.5 text-base font-normal'>%</span>
            </div>
          ) : (
            <div className='text-muted-foreground/60 font-mono text-[28px] font-semibold tracking-tight'>
              —
            </div>
          )}
          <div className='text-muted-foreground mt-1 text-[10px] uppercase tracking-wider'>
            {isOnline ? t('Availability') : t('Offline')}
          </div>
        </div>
      </div>

      {/* Status timeline — focal proof */}
      <div className='mt-5'>
        {group.history && group.history.length > 0 ? (
          <StatusTimeline history={group.history} segmentCount={32} compact />
        ) : (
          <div className='bg-muted/40 text-muted-foreground/80 flex h-[22px] items-center justify-center rounded-md text-[10px]'>
            {t('No history yet')}
          </div>
        )}
      </div>

      {/* Footer: inline stats */}
      <div className='text-muted-foreground mt-4 flex items-center justify-between text-[11px]'>
        <div className='flex items-center gap-3'>
          <span className='inline-flex items-center gap-1'>
            <Zap className='text-muted-foreground/60 h-2.5 w-2.5' />
            <span className='font-mono'>{formatFRT(frt)}</span>
          </span>
          <span className='inline-flex items-center gap-1'>
            <Database className='text-muted-foreground/60 h-2.5 w-2.5' />
            <span className='font-mono'>
              {showCache ? `${cacheRate.toFixed(1)}%` : '—'}
            </span>
          </span>
        </div>
        <div className='flex items-center gap-3'>
          {group.total_channels != null && (
            <span>
              <span className='text-foreground/80 font-mono'>
                {group.online_channels ?? 0}
              </span>
              <span className='text-muted-foreground/60'>
                /{group.total_channels}
              </span>
            </span>
          )}
          {group.updated_at && group.updated_at > 0 && (
            <span className='text-muted-foreground/60 font-mono'>
              {formatClock(group.updated_at)}
            </span>
          )}
        </div>
      </div>
    </button>
  )
}
