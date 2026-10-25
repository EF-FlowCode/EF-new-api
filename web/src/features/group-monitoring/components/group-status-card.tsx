import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { formatMetricTime, performanceColor } from '../lib/model-performance'
import type { GroupSummary } from '../types'
import {
  ModelPerformanceTable,
  type ModelPerformanceTableProps,
} from './model-performance-table'
import { StatusTimeline } from './status-timeline'

type Props = ModelPerformanceTableProps & {
  group: GroupSummary
  onClick?: (group: GroupSummary) => void
}

export function GroupStatusCard({ group, onClick, ...performance }: Props) {
  const { t } = useTranslation()
  const isOnline = group.is_online ?? (group.online_channels ?? 0) > 0
  const availability =
    group.availability_rate != null && group.availability_rate >= 0
      ? group.availability_rate
      : null
  const cacheRate =
    group.cache_hit_rate != null && group.cache_hit_rate >= 0
      ? group.cache_hit_rate
      : null
  const clock = group.updated_at
    ? new Date(group.updated_at * 1000).toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      })
    : ''

  return (
    <Card className='min-w-0 gap-3 rounded-xl py-4'>
      <CardHeader className='gap-3 px-4'>
        <div className='flex flex-wrap items-center gap-x-4 gap-y-2'>
          <CardTitle className='max-w-full min-w-0 text-sm'>
            <div className='flex min-w-0 items-center gap-2'>
              <span
                className='size-2 shrink-0 rounded-full'
                style={{
                  background: isOnline
                    ? 'var(--monitoring-healthy)'
                    : 'var(--monitoring-outage)',
                }}
                role='img'
                aria-label={isOnline ? t('Online') : t('Offline')}
              />
              {onClick ? (
                <button
                  type='button'
                  className='focus-visible:outline-ring truncate text-left hover:underline'
                  title={group.group_name}
                  onClick={() => onClick(group)}
                >
                  {group.group_name}
                </button>
              ) : (
                <span className='truncate' title={group.group_name}>
                  {group.group_name}
                </span>
              )}
            </div>
          </CardTitle>
          <CardDescription className='text-xs'>
            {group.group_ratio != null
              ? t('Group multiplier: {{ratio}}x', { ratio: group.group_ratio })
              : '—'}
          </CardDescription>
          {onClick && group.total_channels != null && (
            <span className='text-muted-foreground font-mono text-xs'>
              {`${group.online_channels ?? 0}/${group.total_channels} ${t('Channels')}`}
            </span>
          )}
          <div className='ml-auto flex shrink-0 items-center gap-3'>
            <span className='text-muted-foreground font-mono text-xs'>
              {clock}
            </span>
            {onClick && (
              <Button
                className='h-7 px-2'
                variant='ghost'
                size='sm'
                onClick={() => onClick(group)}
              >
                {t('Details')}
              </Button>
            )}
          </div>
        </div>
        <div className='flex flex-wrap items-center gap-x-6 gap-y-3'>
          <dl className='grid w-full grid-cols-3 gap-3 md:w-auto md:gap-6'>
            <div className='flex flex-col gap-1 md:flex-row md:items-baseline md:gap-2'>
              <dt className='text-muted-foreground text-xs'>
                {t('Group availability')}
              </dt>
              <dd
                className='font-mono text-sm font-semibold'
                style={{
                  color:
                    availability == null
                      ? undefined
                      : performanceColor(availability),
                }}
              >
                {availability == null ? '—' : `${availability.toFixed(1)}%`}
              </dd>
            </div>
            <div className='flex flex-col gap-1 md:flex-row md:items-baseline md:gap-2'>
              <dt className='text-muted-foreground text-xs'>
                {t('First token')}
              </dt>
              <dd className='font-mono text-sm'>
                {formatMetricTime(group.avg_frt ?? group.first_response_time)}
              </dd>
            </div>
            <div className='flex flex-col gap-1 md:flex-row md:items-baseline md:gap-2'>
              <dt className='text-muted-foreground text-xs'>
                {t('Cache hit')}
              </dt>
              <dd className='font-mono text-sm'>
                {cacheRate == null ? '—' : `${cacheRate.toFixed(1)}%`}
              </dd>
            </div>
          </dl>
          <div className='w-full min-w-0 md:w-64'>
            <StatusTimeline history={group.history} segmentCount={32} compact />
          </div>
        </div>
      </CardHeader>
      <CardContent className='min-w-0 border-t px-4 pt-3'>
        <ModelPerformanceTable {...performance} />
      </CardContent>
    </Card>
  )
}
