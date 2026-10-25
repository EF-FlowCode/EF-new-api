import { useEffect, useState } from 'react'
import { Activity, Database, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { Badge } from '@/components/ui/badge'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fetchAdminGroupDetail, fetchGroupHistory } from '../api'
import type {
  ChannelStat,
  GroupDetail,
  GroupSummary,
  HistoryPoint,
} from '../types'
import { AvailabilityCacheChart } from './availability-cache-chart'
import { StatusTimeline } from './status-timeline'

function rateColor(rate: number | null | undefined): string {
  if (rate == null || rate < 0) return 'var(--muted-foreground)'
  if (rate >= 99) return 'rgb(34 197 94)'
  if (rate >= 95) return 'rgb(132 204 22)'
  if (rate >= 80) return 'rgb(245 158 11)'
  return 'rgb(239 68 68)'
}

// common.ChannelStatusEnabled
const CHANNEL_STATUS_ENABLED = 1

function rateBadgeClass(rate: number | null | undefined): string {
  if (rate == null || rate < 0) {
    return 'border-muted-foreground/40 bg-muted/40 text-muted-foreground'
  }
  if (rate >= 99) {
    return 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
  }
  if (rate >= 95) {
    return 'border-lime-500/40 bg-lime-500/10 text-lime-600 dark:text-lime-400'
  }
  if (rate >= 80) {
    return 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
  }
  return 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
}

function formatFRT(ms: number | null | undefined): string {
  if (ms == null || ms <= 0) return '—'
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  return `${Math.round(ms)}ms`
}

function formatUpdatedAt(seconds: number | null | undefined): string {
  if (!seconds) return '—'
  const date = new Date(seconds * 1000)
  if (Number.isNaN(date.getTime())) return '—'
  return date.toLocaleString([], {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

type StatProps = {
  icon: React.ReactNode
  label: string
  value: string
  valueColor?: string
}

function Stat({ icon, label, value, valueColor }: StatProps) {
  return (
    <div className='bg-card rounded-xl border p-3'>
      <div className='text-muted-foreground flex items-center gap-1.5'>
        {icon}
        <span className='text-[10px] font-semibold tracking-wider uppercase'>
          {label}
        </span>
      </div>
      <div
        className='mt-1.5 font-mono text-xl leading-none font-semibold'
        style={{ color: valueColor }}
      >
        {value}
      </div>
    </div>
  )
}

type Props = {
  open: boolean
  group: GroupSummary | null
  onClose: () => void
}

export function GroupDetailPanel({ open, group, onClose }: Props) {
  const { t } = useTranslation()
  const admin = useIsAdmin()
  const [detail, setDetail] = useState<GroupDetail | null>(null)
  const [history, setHistory] = useState<HistoryPoint[]>([])
  const [loading, setLoading] = useState(false)
  const [intervalMinutes, setIntervalMinutes] = useState(5)

  useEffect(() => {
    if (!open || !group) return
    // async fetch 之前的合法同步 loading/重置——上游变化时清掉旧数据
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLoading(true)
    setDetail(null)
    setHistory([])

    const tasks: Promise<unknown>[] = []
    if (admin) {
      tasks.push(
        fetchAdminGroupDetail(group.group_name)
          .then((d) => setDetail(d))
          .catch(() => {
            /* silent */
          })
      )
    }
    tasks.push(
      fetchGroupHistory(admin, group.group_name)
        .then(({ history: h, intervalMinutes: itv }) => {
          setHistory(h || [])
          if (itv) setIntervalMinutes(itv)
        })
        .catch(() => {
          /* silent */
        })
    )
    Promise.all(tasks).finally(() => setLoading(false))
  }, [open, group, admin])

  if (!group) {
    return (
      <Sheet open={open} onOpenChange={(o) => !o && onClose()}>
        <SheetContent />
      </Sheet>
    )
  }

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
  const channelData: ChannelStat[] = detail?.channel_stats ?? []

  return (
    <Sheet open={open} onOpenChange={(o) => !o && onClose()}>
      <SheetContent className='flex w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-xl'>
        <SheetHeader className='border-b px-4 py-3 sm:px-6 sm:py-4'>
          <SheetTitle className='flex items-center gap-2'>
            <Badge
              variant='outline'
              className={cn(
                'font-normal',
                isOnline
                  ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                  : 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
              )}
            >
              {isOnline ? t('Online') : t('Offline')}
            </Badge>
            <span className='font-medium'>{group.group_name}</span>
            <span className='text-muted-foreground text-xs font-normal'>
              {t('Group detail')}
            </span>
          </SheetTitle>
        </SheetHeader>

        <div className='flex-1 space-y-5 overflow-y-auto p-4 sm:p-5'>
          {/* Hero metrics */}
          <div className='grid grid-cols-3 gap-3'>
            <Stat
              icon={<Activity className='h-3 w-3' />}
              label={t('Availability')}
              value={availRate != null ? `${availRate.toFixed(1)}%` : 'N/A'}
              valueColor={rateColor(availRate)}
            />
            <Stat
              icon={<Zap className='h-3 w-3' />}
              label='FRT'
              value={formatFRT(group.avg_frt ?? group.first_response_time)}
            />
            <Stat
              icon={<Database className='h-3 w-3' />}
              label={t('Cache hit')}
              value={showCache ? `${cacheRate.toFixed(1)}%` : '—'}
              valueColor={showCache ? rateColor(cacheRate) : undefined}
            />
          </div>

          {/* Status timeline */}
          <div>
            <div className='mb-2 text-sm font-semibold'>
              {t('Status timeline')}
            </div>
            {history.length > 0 ? (
              <StatusTimeline history={history} segmentCount={60} />
            ) : (
              <div className='border-muted-foreground/30 text-muted-foreground flex h-12 items-center justify-center rounded-md border border-dashed text-xs'>
                {t('No history yet')}
              </div>
            )}
          </div>

          {/* Trend chart */}
          {history.length > 0 && (
            <div>
              <div className='mb-2 text-sm font-semibold'>{t('Trend')}</div>
              <div className='bg-card rounded-xl border p-3'>
                <AvailabilityCacheChart
                  history={history}
                  intervalMinutes={intervalMinutes}
                />
              </div>
            </div>
          )}

          {/* Channel table — admin only */}
          {admin && (
            <div>
              <div className='mb-2 text-sm font-semibold'>{t('Channels')}</div>
              {loading && channelData.length === 0 ? (
                <div className='space-y-2'>
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className='h-10 w-full' />
                  ))}
                </div>
              ) : channelData.length === 0 ? (
                <div className='text-muted-foreground py-6 text-center text-sm'>
                  {t('No channel data')}
                </div>
              ) : (
                <div className='overflow-x-auto rounded-md border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className='w-16'>{t('ID')}</TableHead>
                        <TableHead>{t('Channel')}</TableHead>
                        <TableHead className='w-20'>{t('Status')}</TableHead>
                        <TableHead className='w-24'>
                          {t('Availability')}
                        </TableHead>
                        <TableHead className='w-24'>{t('Cache hit')}</TableHead>
                        <TableHead className='w-20'>FRT</TableHead>
                        <TableHead className='w-32'>{t('Updated')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {channelData
                        .slice()
                        .sort(
                          (a, b) =>
                            (a.availability_rate ?? 0) -
                            (b.availability_rate ?? 0)
                        )
                        .map((ch) => (
                          <TableRow key={ch.channel_id}>
                            <TableCell className='font-mono'>
                              {ch.channel_id}
                            </TableCell>
                            <TableCell
                              className='max-w-[160px] truncate'
                              title={ch.channel_name}
                            >
                              {ch.channel_name || '—'}
                            </TableCell>
                            <TableCell>
                              <Badge
                                variant='outline'
                                className={cn(
                                  'font-normal',
                                  ch.channel_status === CHANNEL_STATUS_ENABLED
                                    ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
                                    : 'border-muted-foreground/40 bg-muted/40 text-muted-foreground'
                                )}
                              >
                                {ch.channel_status === CHANNEL_STATUS_ENABLED
                                  ? t('Enabled')
                                  : t('Disabled')}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              <Badge
                                variant='outline'
                                className={cn(
                                  'font-normal',
                                  rateBadgeClass(ch.availability_rate)
                                )}
                              >
                                {ch.availability_rate != null &&
                                ch.availability_rate >= 0
                                  ? `${ch.availability_rate.toFixed(1)}%`
                                  : '—'}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              <Badge
                                variant='outline'
                                className={cn(
                                  'font-normal',
                                  ch.cache_hit_rate != null &&
                                    ch.cache_hit_rate >= 3
                                    ? rateBadgeClass(ch.cache_hit_rate)
                                    : 'border-muted-foreground/40 bg-muted/40 text-muted-foreground'
                                )}
                              >
                                {ch.cache_hit_rate != null &&
                                ch.cache_hit_rate >= 3
                                  ? `${ch.cache_hit_rate.toFixed(1)}%`
                                  : '—'}
                              </Badge>
                            </TableCell>
                            <TableCell className='font-mono text-xs'>
                              {formatFRT(ch.avg_frt)}
                            </TableCell>
                            <TableCell className='text-muted-foreground text-xs'>
                              {formatUpdatedAt(ch.updated_at)}
                            </TableCell>
                          </TableRow>
                        ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}
