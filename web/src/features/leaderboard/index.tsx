import { useEffect, useMemo, useState } from 'react'
import { RefreshCw, Trophy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { PageShell } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import { getLobeIcon } from '@/lib/lobe-icon'
import { getPricing } from '@/features/pricing/api'
import { getPerfMetricsSummary } from '@/features/performance-metrics/api'
import {
  ModelPerfBadge,
  type ModelPerfBadgeData,
} from '@/features/pricing/components/model-perf-badge'
import { getModelRanking } from './api'
import { fmtBeijing, formatTrend } from './lib'
import { MiniSparkline } from './mini-sparkline'
import type {
  LeaderboardRange,
  ModelRankingEntry,
  ModelRankingPayload,
} from './types'

// eflow: 标签如实化——后端是滚动窗口(now-7d/30d/365d)，并非自然周/月/年，
// 故改用「近 N 天 / 近 1 年」措辞，消除「本周=本自然周」的误解。
const RANGES: { value: LeaderboardRange; labelKey: string }[] = [
  { value: 'today', labelKey: 'Today' },
  { value: 'week', labelKey: 'Last 7 Days' },
  { value: 'month', labelKey: 'Last 30 Days' },
  { value: 'year', labelKey: 'Last 1 Year' },
  { value: 'all', labelKey: 'All Time' },
]

interface VendorInfo {
  icon?: string
  vendor?: string
}

function SparkCol({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <div className='flex flex-col items-end gap-0.5'>
      <div className='text-muted-foreground/55 text-[10px] leading-4'>
        {label}
      </div>
      {children}
    </div>
  )
}

function RankingRow({
  entry,
  vendor,
  perf,
  latencySeries,
  rankSeries,
  trendBaseline,
  noTrendLabel,
  noTrendTip,
}: {
  entry: ModelRankingEntry
  vendor?: VendorInfo
  perf?: ModelPerfBadgeData
  latencySeries?: number[]
  rankSeries?: number[]
  // eflow: 涨跌列的悬浮提示——「相对哪个区间」的人类可读说明。
  trendBaseline?: string
  // eflow: 无涨跌基准时的占位文案 + 提示(新上榜 / 全部时间无上一周期)。
  noTrendLabel?: string
  noTrendTip?: string
}) {
  const { t } = useTranslation()
  const trend = entry.trend_known ? formatTrend(entry.trend_pct) : null
  return (
    <div className='flex items-center gap-3 py-2.5'>
      <div className='text-muted-foreground/70 w-6 shrink-0 text-right text-sm tabular-nums'>
        {entry.rank}.
      </div>
      <div className='bg-muted/40 flex size-7 shrink-0 items-center justify-center overflow-hidden rounded-lg'>
        {vendor?.icon ? (
          getLobeIcon(vendor.icon, 24)
        ) : (
          <span className='text-muted-foreground text-xs font-bold'>
            {entry.model_name?.charAt(0).toUpperCase() || '?'}
          </span>
        )}
      </div>
      <div className='min-w-0 flex-1'>
        <div className='truncate font-mono text-sm font-medium'>
          {entry.model_name}
        </div>
        {vendor?.vendor && (
          <div className='text-muted-foreground/70 truncate text-xs italic'>
            by {vendor.vendor}
          </div>
        )}
      </div>
      <ModelPerfBadge perf={perf} className='mr-1 shrink-0' />
      {/* eflow: 行内迷你波形——延迟趋势(性能,已公开指标) + 名次走势(纯相对量)。
          二者都不涉及绝对用量,脱敏不破。窄屏隐藏以免挤爆稠密行。 */}
      <div className='hidden shrink-0 items-end gap-3 min-[1024px]:flex'>
        <SparkCol label={t('Perf')}>
          <MiniSparkline
            values={latencySeries ?? []}
            tone='perf'
            ariaLabel={t('Latency trend over time')}
          />
        </SparkCol>
        <SparkCol label={t('Rank')}>
          <MiniSparkline
            values={rankSeries ?? []}
            tone='rank'
            invert
            ariaLabel={t('Ranking position over time')}
          />
        </SparkCol>
      </div>
      {/* eflow: 脱敏排行榜——不展示绝对 token 用量(防反推流水),只留相对涨跌%。
          真实用量在 admin-api 后台看。 */}
      {trend ? (
        <div
          title={trendBaseline}
          className={`w-12 shrink-0 text-right text-xs tabular-nums ${trend.up ? 'text-emerald-600 dark:text-emerald-400' : 'text-rose-500'}`}
        >
          {trend.label}
        </div>
      ) : (
        // eflow: 无基准不再留白(会被误以为漏数据)——给个弱化占位 + 悬浮解释。
        <div
          title={noTrendTip}
          className='text-muted-foreground/45 w-12 shrink-0 text-right text-xs'
        >
          {noTrendLabel}
        </div>
      )}
    </div>
  )
}

// eflow: 区间上下文条的两段文案——「统计区间」与「涨跌对比基准」。
// 全部据后端 window_start / prev_start / generated_at 算，单一真相源，
// 不会和后端窗口口径漂移。日期一律按东八区渲染。
function rangeContextText(
  range: LeaderboardRange,
  data: ModelRankingPayload | null,
  t: (k: string) => string
): { windowText: string; prevText: string } {
  if (!data) return { windowText: '—', prevText: '—' }
  if (range === 'all') {
    return { windowText: t('All history'), prevText: t('No comparable previous period') }
  }
  if (range === 'today') {
    return {
      windowText: `${t('Today')} 00:00 → ${t('now')}`,
      prevText: t('Yesterday, same elapsed window'),
    }
  }
  return {
    windowText: `${fmtBeijing(data.window_start)} → ${fmtBeijing(data.generated_at)}`,
    prevText: `${fmtBeijing(data.prev_start)} ~ ${fmtBeijing(data.window_start)}`,
  }
}

// 区间上下文条内的一段「键: 值」。
function CtxSeg({ k, children }: { k: string; children: React.ReactNode }) {
  return (
    <span className='flex items-center gap-1.5'>
      <span className='text-muted-foreground/55'>{k}</span>
      <span className='text-foreground/85 font-medium'>{children}</span>
    </span>
  )
}

export function Leaderboard() {
  const { t } = useTranslation()
  const [range, setRange] = useState<LeaderboardRange>('week')
  const [data, setData] = useState<ModelRankingPayload | null>(null)
  const [loading, setLoading] = useState(true)
  // eflow: 手动刷新——tick 自增触发下面三个 fetch 的 effect 重跑;
  // refreshing 只驱动按钮转圈,由榜单(ranking) effect 的 finally 收尾。
  const [refreshTick, setRefreshTick] = useState(0)
  const [refreshing, setRefreshing] = useState(false)
  const [vendorMap, setVendorMap] = useState<Map<string, VendorInfo>>(new Map())
  const [perfMap, setPerfMap] = useState<Map<string, ModelPerfBadgeData>>(
    new Map()
  )
  const [latencyMap, setLatencyMap] = useState<Map<string, number[]>>(new Map())
  const [rankSeriesMap, setRankSeriesMap] = useState<Map<string, number[]>>(
    new Map()
  )

  // Vendor icon/name map (reuses the public pricing endpoint), fetched once.
  // 注意:原始 model 的 vendor_icon/vendor_name 是空的——厂商 logo 存在独立的
  // res.vendors[] 里,model 用 vendor_id 引用。必须按 vendor_id join 出来,
  // 否则全部回退首字母(与模型广场 usePricingData 的 enrich 逻辑一致)。
  useEffect(() => {
    getPricing()
      .then((res) => {
        const vendorById = new Map(
          (res?.vendors ?? []).map((v) => [v.id, v])
        )
        const map = new Map<string, VendorInfo>()
        for (const m of res?.data ?? []) {
          const vendor = m.vendor_id ? vendorById.get(m.vendor_id) : undefined
          map.set(m.model_name, {
            icon: vendor?.icon ?? m.vendor_icon,
            vendor: vendor?.name ?? m.vendor_name,
          })
        }
        setVendorMap(map)
      })
      .catch(() => {})
  }, [refreshTick])

  // Perf 徽章数据(后端 GetPerfMetrics，最近 24h，与榜单区间无关)，拉一次。
  useEffect(() => {
    getPerfMetricsSummary(24)
      .then((res) => {
        const map = new Map<string, ModelPerfBadgeData>()
        const lat = new Map<string, number[]>()
        for (const m of res?.data?.models ?? []) {
          map.set(m.model_name, m)
          if (m.latency_series && m.latency_series.length > 0) {
            lat.set(m.model_name, m.latency_series)
          }
        }
        setPerfMap(map)
        setLatencyMap(lat)
      })
      .catch(() => {})
  }, [refreshTick])

  useEffect(() => {
    let active = true
    setLoading(true)
    getModelRanking(range)
      .then((res) => {
        if (!active) return
        if (res.success && res.data) {
          setData(res.data)
          // 名次走势按模型分组,后端已按 time 升序发出,这里保险再排一次。
          const byModel = new Map<string, { time: number; rank: number }[]>()
          for (const p of res.data.rank_series ?? []) {
            const arr = byModel.get(p.model_name) ?? []
            arr.push({ time: p.time, rank: p.rank })
            byModel.set(p.model_name, arr)
          }
          const ranks = new Map<string, number[]>()
          for (const [name, arr] of byModel) {
            arr.sort((a, b) => a.time - b.time)
            ranks.set(
              name,
              arr.map((x) => x.rank)
            )
          }
          setRankSeriesMap(ranks)
        } else {
          setData(null)
          setRankSeriesMap(new Map())
        }
      })
      .catch(() => {
        if (active) {
          setData(null)
          setRankSeriesMap(new Map())
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false)
          setRefreshing(false)
        }
      })
    return () => {
      active = false
    }
  }, [range, refreshTick])

  const handleRefresh = () => {
    if (refreshing) return
    setRefreshing(true)
    setRefreshTick((n) => n + 1)
  }

  const half = useMemo(() => {
    const list = data?.ranking ?? []
    const mid = Math.ceil(list.length / 2)
    return [list.slice(0, mid), list.slice(mid)]
  }, [data])

  // eflow: 区间上下文（真实起止 + 涨跌基准）+ 涨跌列悬浮提示。
  const ctx = useMemo(() => rangeContextText(range, data, t), [range, data, t])
  const trendBaseline = `${t('Usage change vs')} ${ctx.prevText}`
  // eflow: 无涨跌基准的占位——全部时间视图本就无上一周期；其余区间则是「新上榜」。
  const noTrendLabel = range === 'all' ? '—' : t('new')
  const noTrendTip =
    range === 'all'
      ? t('All-time view has no previous period to compare')
      : t('No usage in the previous period — newly listed')

  return (
    <PageShell contained={false}>
      <div className='mx-auto w-full max-w-[1400px] px-3 pt-6 pb-10 sm:px-6 sm:pt-8 xl:px-8'>
        {/* Range tabs */}
        <div className='mb-3 flex flex-wrap items-center gap-1 border-b'>
          {RANGES.map((r) => (
            <button
              key={r.value}
              type='button'
              onClick={() => setRange(r.value)}
              className={`-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors ${
                range === r.value
                  ? 'border-foreground text-foreground'
                  : 'text-muted-foreground hover:text-foreground border-transparent'
              }`}
            >
              {t(r.labelKey)}
            </button>
          ))}
          {/* eflow: 手动刷新按钮,对齐监控页样式(ghost + RefreshCw 旋转) */}
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='-mb-px ml-auto'
            onClick={handleRefresh}
            disabled={refreshing}
            title={t('Refresh')}
          >
            <RefreshCw
              className={cn('h-3.5 w-3.5', refreshing && 'animate-spin')}
            />
          </Button>
        </div>

        {/* eflow: 区间上下文条——明示当前标签对应的真实统计起止 + 涨跌对比基准，
            并点明实测性能恒为近 24h、时区为东八区，根治「时间不明确」。 */}
        <div className='bg-muted/30 text-muted-foreground flex flex-wrap items-center gap-x-5 gap-y-1.5 rounded-lg border px-3.5 py-2 text-xs'>
          <CtxSeg k={t('Window')}>
            <span className='tabular-nums'>{ctx.windowText}</span>
          </CtxSeg>
          <span className='bg-muted-foreground/30 size-1 rounded-full' />
          <CtxSeg k={t('Trend vs')}>
            <span className='tabular-nums'>{ctx.prevText}</span>
          </CtxSeg>
          <span className='bg-muted-foreground/30 size-1 rounded-full' />
          <CtxSeg k={t('Measured perf')}>{t('always last 24h')}</CtxSeg>
          <span className='bg-muted-foreground/30 size-1 rounded-full' />
          <CtxSeg k={t('Timezone')}>UTC+8</CtxSeg>
        </div>

        {/* eflow: 脱敏排行榜不再展示「热门模型」用量曲线与平台总量(防反推流水);
            真实用量在 admin-api 后台查看。此处直接进入相对排名榜。 */}

        {/* LLM leaderboard */}
        <div className='mt-4 rounded-xl border p-4 sm:p-6'>
          <div className='mb-3 flex items-center gap-2'>
            <Trophy className='size-5 text-amber-500' />
            <div>
              <div className='text-base font-semibold'>{t('LLM Leaderboard')}</div>
              <div className='text-muted-foreground text-xs'>
                {t('Most-used models on this platform')}
              </div>
            </div>
          </div>

          {/* eflow: 口径图例——把右侧列归到两套时间尺度：实测列恒为 24h、名次/涨跌随区间。
              小样本对齐风险高的「列头网格」不做，改用稳健的图例 + 上方上下文条表达。 */}
          <div className='text-muted-foreground/70 mb-1 hidden flex-wrap items-center gap-x-3 gap-y-1 text-[11px] min-[1024px]:flex'>
            <span>
              <span className='font-medium text-sky-600 dark:text-sky-400'>
                {t('Last 24h measured')}
              </span>
              {t(': latency · throughput · status · perf')}
            </span>
            <span className='opacity-40'>│</span>
            <span>
              <span className='font-medium text-amber-600 dark:text-amber-400'>
                {t('Selected range')}
              </span>
              {t(': rank · change')}
            </span>
          </div>

          {loading ? (
            <div className='text-muted-foreground/60 py-12 text-center text-sm'>
              {t('Loading...')}
            </div>
          ) : (data?.ranking.length ?? 0) === 0 ? (
            <div className='text-muted-foreground/60 py-12 text-center text-sm'>
              {t('No data yet')}
            </div>
          ) : (
            <div className='grid grid-cols-1 gap-x-10 md:grid-cols-2'>
              {half.map((col, i) => (
                <div key={i} className='divide-border/60 divide-y'>
                  {col.map((entry) => (
                    <RankingRow
                      key={entry.model_name}
                      entry={entry}
                      vendor={vendorMap.get(entry.model_name)}
                      perf={perfMap.get(entry.model_name)}
                      latencySeries={latencyMap.get(entry.model_name)}
                      rankSeries={rankSeriesMap.get(entry.model_name)}
                      trendBaseline={trendBaseline}
                      noTrendLabel={noTrendLabel}
                      noTrendTip={noTrendTip}
                    />
                  ))}
                </div>
              ))}
            </div>
          )}

          {/* eflow: 数据口径说明——让用户看懂 延迟/吞吐/状态 与涨跌% 的含义和波动性 */}
          <p className='text-muted-foreground/60 mt-4 border-t pt-3 text-[11px] leading-relaxed'>
            {t(
              'Latency / throughput / status are measured over the last 24h (limited samples may fluctuate). The percentage is the usage change vs the previous period — desensitized, no absolute volume shown; short periods or small baselines can swing sharply.'
            )}{' '}
            {t(
              'The sparklines show each model’s recent latency trend and ranking movement; the same small-sample volatility applies.'
            )}
          </p>
        </div>
      </div>
    </PageShell>
  )
}
