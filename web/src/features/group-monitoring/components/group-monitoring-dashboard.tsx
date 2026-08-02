import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  ChevronDown,
  RefreshCw,
  Search,
  Settings2,
  X,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import {
  fetchGroupHistory,
  fetchGroups,
  refreshAdminMonitoring,
  updateGroupMonitoringSettings,
} from '../api'
import type { GroupListMeta, GroupSummary } from '../types'
import { GroupDetailPanel } from './group-detail-panel'
import { GroupStatusCard } from './group-status-card'

const UNCATEGORIZED = '__uncat__'

const POLL_INTERVAL_MS = 60 * 1000
const SORT_KEY = 'monitoring-sort-mode'

type SortMode = 'status' | 'name' | 'availability'

const SORT_OPTIONS: Array<{ value: SortMode; labelKey: string }> = [
  { value: 'status', labelKey: 'By status' },
  { value: 'name', labelKey: 'By name' },
  { value: 'availability', labelKey: 'By availability' },
]

function compareGroups(
  a: GroupSummary,
  b: GroupSummary,
  mode: SortMode
): number {
  const aOnline = a.is_online ?? (a.online_channels ?? 0) > 0
  const bOnline = b.is_online ?? (b.online_channels ?? 0) > 0
  switch (mode) {
    case 'name':
      return (a.group_name || '').localeCompare(b.group_name || '')
    case 'availability': {
      const ar = a.availability_rate ?? -1
      const br = b.availability_rate ?? -1
      return ar - br
    }
    case 'status':
    default:
      if (aOnline !== bOnline) return aOnline ? 1 : -1
      return (a.availability_rate ?? 100) - (b.availability_rate ?? 100)
  }
}

function avgAvailability(groups: GroupSummary[]): number | null {
  const valid = groups
    .map((g) => g.availability_rate)
    .filter((r): r is number => r != null && r >= 0)
  if (valid.length === 0) return null
  return valid.reduce((s, v) => s + v, 0) / valid.length
}

function rateAccent(rate: number | null | undefined): string {
  if (rate == null || rate < 0) return 'var(--muted-foreground)'
  if (rate >= 99) return 'rgb(34 197 94)'
  if (rate >= 95) return 'rgb(132 204 22)'
  if (rate >= 80) return 'rgb(245 158 11)'
  return 'rgb(239 68 68)'
}

export function GroupMonitoringDashboard() {
  const { t } = useTranslation()
  const admin = useIsAdmin()

  const [groups, setGroups] = useState<GroupSummary[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [selectedGroup, setSelectedGroup] = useState<GroupSummary | null>(null)
  const [detailVisible, setDetailVisible] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [sortMode, setSortMode] = useState<SortMode>(() => {
    try {
      const stored = localStorage.getItem(SORT_KEY) as SortMode | null
      return stored && ['status', 'name', 'availability'].includes(stored)
        ? stored
        : 'status'
    } catch {
      return 'status'
    }
  })
  const [countdown, setCountdown] = useState(POLL_INTERVAL_MS)
  const initialLoad = useRef(true)
  const [meta, setMeta] = useState<GroupListMeta>({
    categories: {},
    category_order: [],
  })
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [manageOpen, setManageOpen] = useState(false)

  const loadGroups = useCallback(
    async (includeHistory: boolean) => {
      try {
        const { groups: summaries, meta: nextMeta } = await fetchGroups(admin)

        let withHistory = summaries
        if (includeHistory && summaries.length > 0) {
          withHistory = await Promise.all(
            summaries.map(async (g) => {
              try {
                const { history, intervalMinutes } = await fetchGroupHistory(
                  admin,
                  g.group_name
                )
                return {
                  ...g,
                  history,
                  aggregation_interval_minutes: intervalMinutes,
                }
              } catch {
                return g
              }
            })
          )
        }

        setGroups((prev) => {
          if (!includeHistory && prev.length > 0) {
            return withHistory.map((g) => {
              const old = prev.find((p) => p.group_name === g.group_name)
              if (old) {
                return {
                  ...g,
                  history: old.history,
                  aggregation_interval_minutes:
                    old.aggregation_interval_minutes,
                }
              }
              return g
            })
          }
          return withHistory
        })
        setMeta(nextMeta)
      } catch (e) {
        toast.error(
          e instanceof Error ? e.message : t('Failed to load monitoring data')
        )
      }
    },
    [admin, t]
  )

  // Initial load
  useEffect(() => {
    let active = true
    const init = async () => {
      setLoading(true)
      await loadGroups(true)
      if (!active) return
      setLoading(false)
      initialLoad.current = false
      setCountdown(POLL_INTERVAL_MS)
    }
    init()
    return () => {
      active = false
    }
  }, [loadGroups])

  // Background polling
  useEffect(() => {
    const id = setInterval(() => {
      if (!initialLoad.current) {
        loadGroups(false)
        setCountdown(POLL_INTERVAL_MS)
      }
    }, POLL_INTERVAL_MS)
    return () => clearInterval(id)
  }, [loadGroups])

  // Countdown tick
  useEffect(() => {
    const id = setInterval(() => {
      setCountdown((c) => Math.max(0, c - 1000))
    }, 1000)
    return () => clearInterval(id)
  }, [])

  const handleRefresh = async () => {
    setRefreshing(true)
    try {
      if (admin) {
        const ok = await refreshAdminMonitoring()
        if (ok) toast.success(t('Refreshed'))
      }
      await loadGroups(true)
      setCountdown(POLL_INTERVAL_MS)
    } catch {
      toast.error(t('Refresh failed'))
    } finally {
      setRefreshing(false)
    }
  }

  const handleSort = (val: SortMode) => {
    setSortMode(val)
    try {
      localStorage.setItem(SORT_KEY, val)
    } catch {
      /* ignore */
    }
  }

  const handleCardClick = (group: GroupSummary) => {
    if (!admin) return
    setSelectedGroup(group)
    setDetailVisible(true)
  }

  const visible = useMemo(() => {
    const kw = keyword.trim().toLowerCase()
    const filtered = kw
      ? groups.filter((g) => (g.group_name || '').toLowerCase().includes(kw))
      : groups
    return [...filtered].sort((a, b) => compareGroups(a, b, sortMode))
  }, [groups, keyword, sortMode])

  // eflow: 按分类把可见卡片切块。category_order 优先；不在列表里的分类按字母
  // 排在后面；"未分类"的分组始终最后。如果没设任何分类，整页就是一个块（保留
  // 跟原来视觉一致的 fallback）。
  const sections = useMemo(() => {
    const categories = meta.categories || {}
    const buckets = new Map<string, GroupSummary[]>()
    for (const g of visible) {
      const cat = (categories[g.group_name] || '').trim() || UNCATEGORIZED
      if (!buckets.has(cat)) buckets.set(cat, [])
      buckets.get(cat)!.push(g)
    }
    const ordered: string[] = []
    const seen = new Set<string>()
    for (const c of meta.category_order || []) {
      const key = c.trim()
      if (key && buckets.has(key) && !seen.has(key)) {
        ordered.push(key)
        seen.add(key)
      }
    }
    const rest = Array.from(buckets.keys())
      .filter((k) => !seen.has(k) && k !== UNCATEGORIZED)
      .sort((a, b) => a.localeCompare(b))
    ordered.push(...rest)
    if (buckets.has(UNCATEGORIZED)) ordered.push(UNCATEGORIZED)
    return ordered.map((cat) => ({
      key: cat,
      label: cat === UNCATEGORIZED ? t('Uncategorized') : cat,
      items: buckets.get(cat)!,
    }))
  }, [visible, meta.categories, meta.category_order, t])

  const hasMultipleCategories = sections.length > 1

  const onlineCount = groups.filter(
    (g) => g.is_online ?? (g.online_channels ?? 0) > 0
  ).length
  const offlineCount = groups.length - onlineCount
  const avgAvail = avgAvailability(groups)

  const countdownLabel = `${Math.floor(countdown / 60000)}:${String(
    Math.floor((countdown % 60000) / 1000)
  ).padStart(2, '0')}`

  return (
    <div className='px-4 pb-12 sm:px-8 lg:px-10'>
      <div className='mx-auto w-full max-w-[1440px]'>
        {/* Title bar */}
        <div className='flex flex-wrap items-center justify-between gap-4 py-6 sm:py-8'>
          <div className='flex flex-wrap items-baseline gap-x-4 gap-y-2'>
            <h1 className='m-0 text-xl font-semibold tracking-tight'>
              {t('Group Monitoring')}
            </h1>
            {!loading && groups.length > 0 && (
              <div className='text-muted-foreground flex flex-wrap items-center gap-x-4 gap-y-1 text-xs'>
                <span className='inline-flex items-center gap-1.5'>
                  <span className='inline-block h-1.5 w-1.5 rounded-full bg-emerald-500' />
                  <span className='text-foreground/80 font-mono'>
                    {onlineCount}
                  </span>
                  <span>{t('Online')}</span>
                </span>
                <span className='inline-flex items-center gap-1.5'>
                  <span
                    className='inline-block h-1.5 w-1.5 rounded-full'
                    style={{
                      background:
                        offlineCount > 0 ? 'rgb(239 68 68)' : 'var(--muted)',
                    }}
                  />
                  <span className='text-foreground/80 font-mono'>
                    {offlineCount}
                  </span>
                  <span>{t('Offline')}</span>
                </span>
                {avgAvail != null && (
                  <span className='inline-flex items-baseline gap-1.5'>
                    <span>{t('Avg availability')}</span>
                    <span
                      className='font-mono'
                      style={{ color: rateAccent(avgAvail) }}
                    >
                      {avgAvail.toFixed(1)}%
                    </span>
                  </span>
                )}
              </div>
            )}
          </div>

          <div className='flex flex-wrap items-center gap-2'>
            <div className='relative w-[200px]'>
              <Search className='text-muted-foreground absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2' />
              <Input
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                placeholder={t('Search groups')}
                className='pl-8'
              />
            </div>
            <Select
              value={sortMode}
              onValueChange={(v) => handleSort(v as SortMode)}
            >
              <SelectTrigger className='w-[140px]'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {SORT_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {t(opt.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button
              type='button'
              variant='ghost'
              onClick={admin ? handleRefresh : undefined}
              disabled={!admin || refreshing}
              title={
                admin
                  ? t('Refresh now · next auto refresh in {{c}}', {
                      c: countdownLabel,
                    })
                  : t('Next auto refresh in {{c}}', { c: countdownLabel })
              }
            >
              <RefreshCw
                className={cn(
                  'mr-1.5 h-3.5 w-3.5',
                  refreshing && 'animate-spin'
                )}
              />
              <span className='text-muted-foreground/80 font-mono text-[11px]'>
                {countdownLabel}
              </span>
            </Button>
            {admin && (
              <Button
                type='button'
                variant='outline'
                onClick={() => setManageOpen(true)}
              >
                <Settings2 className='mr-1.5 h-3.5 w-3.5' />
                {t('Manage groups')}
              </Button>
            )}
          </div>
        </div>

        {/* Cards grid */}
        {loading ? (
          <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-5 lg:grid-cols-3 2xl:grid-cols-4'>
            {[1, 2, 3, 4, 5, 6].map((i) => (
              <div key={i} className='bg-card rounded-2xl border p-5'>
                <Skeleton className='mb-4 h-5 w-1/2' />
                <Skeleton className='mb-2 h-3 w-3/4' />
                <Skeleton className='mb-4 h-3 w-2/3' />
                <Skeleton className='h-6 w-full' />
              </div>
            ))}
          </div>
        ) : groups.length === 0 ? (
          <EmptyState
            icon={<Activity className='h-8 w-8 opacity-40' />}
            title={t('No monitored groups')}
            desc={
              admin
                ? t('Configure under System Settings → Group Monitoring')
                : t('No public monitoring groups are available right now.')
            }
          />
        ) : visible.length === 0 ? (
          <EmptyState
            icon={<X className='h-8 w-8 opacity-40' />}
            title={t('No groups match "{{kw}}"', { kw: keyword })}
          />
        ) : hasMultipleCategories ? (
          <div className='space-y-6'>
            {sections.map((section) => {
              const isOpen = !collapsed[section.key]
              return (
                <section key={section.key} className='space-y-3'>
                  <button
                    type='button'
                    onClick={() =>
                      setCollapsed((prev) => ({
                        ...prev,
                        [section.key]: !prev[section.key],
                      }))
                    }
                    className='hover:text-foreground text-muted-foreground flex w-full items-center gap-2 text-left text-sm font-medium transition'
                  >
                    <ChevronDown
                      className={cn(
                        'h-4 w-4 transition-transform',
                        !isOpen && '-rotate-90'
                      )}
                    />
                    <span className='text-foreground/90'>{section.label}</span>
                    <span className='text-muted-foreground/70 text-xs'>
                      {section.items.length}
                    </span>
                  </button>
                  {isOpen && (
                    <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-5 lg:grid-cols-3 2xl:grid-cols-4'>
                      {section.items.map((g) => (
                        <GroupStatusCard
                          key={g.group_name}
                          group={g}
                          onClick={admin ? handleCardClick : undefined}
                        />
                      ))}
                    </div>
                  )}
                </section>
              )
            })}
          </div>
        ) : (
          <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 sm:gap-5 lg:grid-cols-3 2xl:grid-cols-4'>
            {visible.map((g) => (
              <GroupStatusCard
                key={g.group_name}
                group={g}
                onClick={admin ? handleCardClick : undefined}
              />
            ))}
          </div>
        )}

        <GroupDetailPanel
          open={detailVisible}
          group={selectedGroup}
          onClose={() => {
            setDetailVisible(false)
            setSelectedGroup(null)
          }}
        />

        {admin && (
          <ManageGroupsSheet
            open={manageOpen}
            onOpenChange={setManageOpen}
            meta={meta}
            onSaved={() => {
              setManageOpen(false)
              loadGroups(true)
            }}
          />
        )}
      </div>
    </div>
  )
}

function EmptyState({
  icon,
  title,
  desc,
}: {
  icon: React.ReactNode
  title: string
  desc?: string
}) {
  return (
    <div className='border-muted-foreground/30 flex flex-col items-center justify-center rounded-2xl border border-dashed py-24 text-center'>
      <div className='text-muted-foreground mb-4'>{icon}</div>
      <div className='text-foreground/80 text-base font-medium'>{title}</div>
      {desc && (
        <div className='text-muted-foreground mt-1.5 text-sm'>{desc}</div>
      )}
    </div>
  )
}

// eflow: admin 用的"分组管理"drawer。让站长直接在监控页里勾选哪些分组进监
// 控、给每个分组打类别标签、调整类别在前端的渲染顺序。保存时一次 PUT
// /api/option/group_monitoring_setting，覆盖 monitoring_groups / group_categories
// / category_order 三个字段。
function ManageGroupsSheet({
  open,
  onOpenChange,
  meta,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  meta: GroupListMeta
  onSaved: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [enabled, setEnabled] = useState<Record<string, boolean>>({})
  const [categories, setCategories] = useState<Record<string, string>>({})
  const [orderText, setOrderText] = useState('')
  const [saving, setSaving] = useState(false)

  // 每次打开时按服务端最新值重置表单，避免上次留存的脏状态。
  useEffect(() => {
    if (!open) return
    const monitored = new Set(meta.monitored_groups || [])
    const all = meta.all_groups || []
    const initialEnabled: Record<string, boolean> = {}
    for (const g of all) initialEnabled[g] = monitored.has(g)
    setEnabled(initialEnabled)
    setCategories({ ...(meta.categories || {}) })
    setOrderText((meta.category_order || []).join('\n'))
  }, [open, meta])

  const allGroups = meta.all_groups || []
  // 已有类别集合（从 categories map 推），datalist 自动补全用。
  const knownCategories = useMemo(() => {
    const set = new Set<string>()
    Object.values(categories).forEach((c) => {
      const k = (c || '').trim()
      if (k) set.add(k)
    })
    ;(meta.category_order || []).forEach((c) => {
      const k = (c || '').trim()
      if (k) set.add(k)
    })
    return Array.from(set).sort((a, b) => a.localeCompare(b))
  }, [categories, meta.category_order])

  const enabledCount = Object.values(enabled).filter(Boolean).length

  const onSave = async () => {
    setSaving(true)
    try {
      const monitoring_groups = allGroups.filter((g) => enabled[g])
      // 删空 category 字符串避免脏数据落库。
      const group_categories: Record<string, string> = {}
      for (const [g, c] of Object.entries(categories)) {
        const v = (c || '').trim()
        if (v) group_categories[g] = v
      }
      const category_order = orderText
        .split('\n')
        .map((s) => s.trim())
        .filter(Boolean)
      await updateGroupMonitoringSettings({
        monitoring_groups,
        group_categories,
        category_order,
      })
      await queryClient.invalidateQueries({ queryKey: ['status'] })
      toast.success(t('Saved'))
      onSaved()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='flex w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-2xl'>
        <SheetHeader className='border-b px-6 py-4'>
          <SheetTitle>{t('Manage monitored groups')}</SheetTitle>
          <SheetDescription>
            {t(
              'Pick which groups appear on the monitoring page, and tag each group with a category to fold related groups together.'
            )}
          </SheetDescription>
        </SheetHeader>
        <div className='flex-1 space-y-5 overflow-y-auto px-6 py-4'>
          <div>
            <div className='mb-2 flex items-center justify-between'>
              <div className='text-sm font-medium'>
                {t('Groups')}
                <span className='text-muted-foreground/70 ml-2 font-mono text-xs'>
                  {enabledCount}/{allGroups.length}
                </span>
              </div>
            </div>
            <div className='space-y-1.5 rounded-md border'>
              {allGroups.length === 0 ? (
                <div className='text-muted-foreground p-6 text-center text-sm'>
                  {t('No groups configured')}
                </div>
              ) : (
                allGroups.map((g) => (
                  <div
                    key={g}
                    className='even:bg-muted/30 flex items-center gap-3 px-3 py-2'
                  >
                    <Checkbox
                      id={`mon-${g}`}
                      checked={!!enabled[g]}
                      onCheckedChange={(v) =>
                        setEnabled((prev) => ({ ...prev, [g]: !!v }))
                      }
                    />
                    <label
                      htmlFor={`mon-${g}`}
                      className='flex-1 cursor-pointer truncate font-mono text-xs'
                    >
                      {g}
                    </label>
                    <Input
                      list='monitor-cat-suggestions'
                      placeholder={t('Category')}
                      value={categories[g] ?? ''}
                      onChange={(e) =>
                        setCategories((prev) => ({
                          ...prev,
                          [g]: e.target.value,
                        }))
                      }
                      className='h-8 w-44 font-mono text-xs'
                    />
                  </div>
                ))
              )}
            </div>
            <datalist id='monitor-cat-suggestions'>
              {knownCategories.map((c) => (
                <option key={c} value={c} />
              ))}
            </datalist>
          </div>
          <div>
            <div className='mb-2 text-sm font-medium'>
              {t('Category order')}
              <span className='text-muted-foreground ml-2 text-xs font-normal'>
                {t(
                  '(one per line; unlisted categories appear after, alphabetically)'
                )}
              </span>
            </div>
            <Textarea
              value={orderText}
              onChange={(e) => setOrderText(e.target.value)}
              rows={5}
              placeholder={t('e.g.\nClaude 模型\n国产模型\n图像模型')}
              className='font-mono text-xs'
            />
          </div>
        </div>
        <SheetFooter className='border-t px-6 py-3'>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={saving}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={onSave} disabled={saving}>
            {saving ? t('Saving...') : t('Save')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
