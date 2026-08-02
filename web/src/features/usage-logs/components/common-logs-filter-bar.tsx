import { useState, useEffect, useCallback, type ReactNode } from 'react'
import { useNavigate, getRouteApi } from '@tanstack/react-router'
import { useQueryClient, useIsFetching } from '@tanstack/react-query'
import {
  ChevronDown,
  Download,
  Eye,
  EyeOff,
  Loader2,
  RotateCcw,
  Search,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { LOG_TYPES } from '../constants'
import { exportUserLogs, requestOfflineExport } from '../api'
import { ExportLogsDialog } from './dialogs/export-logs-dialog'
import { buildSearchParams } from '../lib/filter'
import {
  buildApiParams,
  buildQueryParams,
  getDefaultTimeRange,
} from '../lib/utils'
import type { CommonLogFilters } from '../types'
import { CompactDateTimeRangePicker } from './compact-date-time-range-picker'
import { useUsageLogsContext } from './usage-logs-provider'

const route = getRouteApi('/_authenticated/usage-logs/$section')
const logTypeValues = ['0', '1', '2', '3', '4', '5', '6'] as const

type LogTypeValue = (typeof logTypeValues)[number]

function isLogTypeValue(value: string): value is LogTypeValue {
  return (logTypeValues as readonly string[]).includes(value)
}

interface CommonLogsFilterBarProps {
  stats?: ReactNode
  viewOptions?: ReactNode
}

export function CommonLogsFilterBar({
  stats,
  viewOptions,
}: CommonLogsFilterBarProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const searchParams = route.useSearch()
  const isAdmin = useIsAdmin()
  const { sensitiveVisible, setSensitiveVisible } = useUsageLogsContext()
  const fetchingLogs = useIsFetching({ queryKey: ['logs'] })

  const [expanded, setExpanded] = useState(false)
  const [filters, setFilters] = useState<CommonLogFilters>(() => {
    const { start, end } = getDefaultTimeRange()
    return { startTime: start, endTime: end }
  })
  const [logType, setLogType] = useState<LogTypeValue | ''>('')
  const [exporting, setExporting] = useState(false)
  const [offlineExporting, setOfflineExporting] = useState(false)
  const [exportDialogOpen, setExportDialogOpen] = useState(false)

  useEffect(() => {
    const next: Partial<CommonLogFilters> = {}
    if (searchParams.startTime)
      next.startTime = new Date(searchParams.startTime)
    if (searchParams.endTime) next.endTime = new Date(searchParams.endTime)
    if (searchParams.channel) next.channel = String(searchParams.channel)
    if (searchParams.model) next.model = searchParams.model
    if (searchParams.token) next.token = searchParams.token
    if (searchParams.group) next.group = searchParams.group
    if (searchParams.username) next.username = searchParams.username
    if (searchParams.userId) next.userId = searchParams.userId
    if (searchParams.requestId) next.requestId = searchParams.requestId

    if (Object.keys(next).length > 0) {
      setFilters((prev) => ({ ...prev, ...next }))
    }

    const typeArr = searchParams.type
    if (Array.isArray(typeArr) && typeArr.length === 1) {
      setLogType(typeArr[0])
    }
  }, [
    searchParams.startTime,
    searchParams.endTime,
    searchParams.channel,
    searchParams.model,
    searchParams.token,
    searchParams.group,
    searchParams.username,
    searchParams.userId,
    searchParams.requestId,
    searchParams.type,
  ])

  const handleChange = useCallback(
    (field: keyof CommonLogFilters, value: Date | string | undefined) => {
      setFilters((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  const handleApply = useCallback(() => {
    const filterParams = buildSearchParams(filters, 'common')
    navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        ...filterParams,
        ...(logType ? { type: [logType] } : {}),
        page: 1,
      },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
    queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
  }, [filters, logType, navigate, queryClient])

  const handleReset = useCallback(() => {
    const { start, end } = getDefaultTimeRange()
    const resetFilters: CommonLogFilters = { startTime: start, endTime: end }
    setFilters(resetFilters)
    setLogType('')

    navigate({
      to: '/usage-logs/$section',
      params: { section: 'common' },
      search: {
        page: 1,
        startTime: start.getTime(),
        endTime: end.getTime(),
      },
    })
    queryClient.invalidateQueries({ queryKey: ['logs'] })
    queryClient.invalidateQueries({ queryKey: ['usage-logs-stats'] })
  }, [navigate, queryClient])

  // eflow: 普通用户自助报账明细导出。点「导出」先弹对话框选时间段（见
  // ExportLogsDialog），确认后用所选区间 + 筛选栏当前其余筛选（模型/分组/
  // Token/类型）导出，后端回 ZIP（明细 + 汇总）。
  const handleExportConfirm = useCallback(
    async (start: Date, end: Date) => {
      setExporting(true)
      try {
        const liveSearchParams = {
          ...buildSearchParams(filters, 'common'),
          ...(logType ? { type: [logType] } : {}),
          startTime: start.getTime(),
          endTime: end.getTime(),
        }
        const params = buildApiParams({
          page: 1,
          pageSize: 0,
          searchParams: liveSearchParams,
          columnFilters: [],
          isAdmin: false,
        }) as Record<string, unknown>
        delete params.p
        delete params.page_size
        const { blob, filename } = await exportUserLogs(
          buildQueryParams(params)
        )
        const url = URL.createObjectURL(blob)
        const a = document.createElement('a')
        a.href = url
        a.download = filename
        document.body.appendChild(a)
        a.click()
        a.remove()
        URL.revokeObjectURL(url)
        setExportDialogOpen(false)
      } catch {
        toast.error(t('Export failed'))
      } finally {
        setExporting(false)
      }
    },
    [filters, logType, t]
  )

  // eflow: 离线导出——同款筛选 + 区间，但后端后台异步生成 ZIP 并发到邮箱（email 为空
  // 则发到绑定邮箱）。适合大区间，避免同步导出长时间占请求/卡死。
  const handleOfflineExportConfirm = useCallback(
    async (start: Date, end: Date, email: string) => {
      setOfflineExporting(true)
      try {
        const liveSearchParams = {
          ...buildSearchParams(filters, 'common'),
          ...(logType ? { type: [logType] } : {}),
          startTime: start.getTime(),
          endTime: end.getTime(),
        }
        const params = buildApiParams({
          page: 1,
          pageSize: 0,
          searchParams: liveSearchParams,
          columnFilters: [],
          isAdmin: false,
        }) as Record<string, unknown>
        delete params.p
        delete params.page_size
        if (email) params.email = email
        const res = await requestOfflineExport(buildQueryParams(params))
        if (res.success) {
          toast.success(res.message || t('Export task submitted'))
          setExportDialogOpen(false)
        } else {
          toast.error(res.message || t('Export failed'))
        }
      } catch {
        toast.error(t('Export failed'))
      } finally {
        setOfflineExporting(false)
      }
    },
    [filters, logType, t]
  )

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (e.key === 'Enter') handleApply()
    },
    [handleApply]
  )

  const hasExpandedFilters =
    !!filters.token ||
    !!filters.username ||
    !!filters.userId ||
    !!filters.channel ||
    !!filters.requestId

  return (
    <>
    <div className='space-y-2 sm:space-y-3'>
      {/* Primary filter row */}
      <div className='grid grid-cols-2 gap-1.5 sm:grid-cols-4 sm:gap-2 lg:grid-cols-[minmax(280px,2fr)_minmax(140px,1fr)_minmax(120px,1fr)_minmax(120px,0.8fr)_auto]'>
        <CompactDateTimeRangePicker
          start={filters.startTime}
          end={filters.endTime}
          onChange={({ start, end }) => {
            handleChange('startTime', start)
            handleChange('endTime', end)
          }}
          className='col-span-2 lg:col-span-1'
        />
        <Input
          placeholder={t('Model Name')}
          value={filters.model || ''}
          onChange={(e) => handleChange('model', e.target.value)}
          onKeyDown={handleKeyDown}
          className='h-9'
        />
        <Input
          placeholder={t('Group')}
          type={sensitiveVisible ? 'text' : 'password'}
          value={filters.group || ''}
          onChange={(e) => handleChange('group', e.target.value)}
          onKeyDown={handleKeyDown}
          className='h-9'
        />
        <Select
          value={logType}
          onValueChange={(value) => {
            setLogType(isLogTypeValue(value) ? value : '')
          }}
        >
          <SelectTrigger className='h-9'>
            <SelectValue placeholder={t('All Types')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('All Types')}</SelectItem>
            {LOG_TYPES.map((type) => (
              <SelectItem key={type.value} value={String(type.value)}>
                {t(type.label)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <button
          type='button'
          className={cn(
            'text-muted-foreground hover:text-foreground flex h-9 items-center gap-1 rounded-md px-2 text-xs transition-colors',
            hasExpandedFilters && !expanded && 'text-primary'
          )}
          onClick={() => setExpanded((p) => !p)}
        >
          <ChevronDown
            className={cn(
              'size-3.5 transition-transform duration-200',
              expanded && 'rotate-180'
            )}
          />
          {expanded ? t('Collapse') : t('Expand')}
        </button>
      </div>

      {/* Expandable filter row */}
      <div
        className={cn(
          'grid gap-2 overflow-hidden transition-all duration-200',
          expanded
            ? 'grid-rows-[1fr] opacity-100'
            : 'grid-rows-[0fr] opacity-0'
        )}
      >
        <div className='min-h-0 overflow-hidden'>
          <div className='grid grid-cols-2 gap-1.5 sm:grid-cols-3 sm:gap-2 lg:grid-cols-5'>
            <Input
              placeholder={t('Token Name')}
              type={sensitiveVisible ? 'text' : 'password'}
              value={filters.token || ''}
              onChange={(e) => handleChange('token', e.target.value)}
              onKeyDown={handleKeyDown}
              className='h-9'
            />
            {isAdmin && (
              <Input
                placeholder={t('Username')}
                type={sensitiveVisible ? 'text' : 'password'}
                value={filters.username || ''}
                onChange={(e) => handleChange('username', e.target.value)}
                onKeyDown={handleKeyDown}
                className='h-9'
              />
            )}
            {isAdmin && (
              <Input
                placeholder={t('User ID')}
                value={filters.userId || ''}
                onChange={(e) => handleChange('userId', e.target.value)}
                onKeyDown={handleKeyDown}
                className='h-9'
              />
            )}
            {isAdmin && (
              <Input
                placeholder={t('Channel ID')}
                value={filters.channel || ''}
                onChange={(e) => handleChange('channel', e.target.value)}
                onKeyDown={handleKeyDown}
                className='h-9'
              />
            )}
            <Input
              placeholder={t('Request ID')}
              value={filters.requestId || ''}
              onChange={(e) => handleChange('requestId', e.target.value)}
              onKeyDown={handleKeyDown}
              className='h-9'
            />
          </div>
        </div>
      </div>

      {/* Actions row */}
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex min-w-0 flex-wrap items-center gap-2 sm:gap-3'>
          {stats && <div className='min-w-0'>{stats}</div>}
        </div>

        <div className='flex shrink-0 items-center gap-2 self-end sm:self-auto'>
          <button
            type='button'
            className='text-muted-foreground hover:text-foreground inline-flex size-8 items-center justify-center rounded-md border transition-colors'
            title={sensitiveVisible ? t('Hide') : t('Show')}
            aria-label={sensitiveVisible ? t('Hide') : t('Show')}
            onClick={() => setSensitiveVisible(!sensitiveVisible)}
          >
            {sensitiveVisible ? (
              <Eye className='size-3.5' />
            ) : (
              <EyeOff className='size-3.5' />
            )}
          </button>
          <Button
            variant='outline'
            size='sm'
            className='h-8'
            title={t('Export current filtered logs as CSV (for reimbursement)')}
            onClick={() => setExportDialogOpen(true)}
          >
            <Download className='size-3.5' />
            {t('Export')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            className='h-8'
            onClick={handleReset}
          >
            <RotateCcw className='size-3.5' />
            {t('Reset')}
          </Button>
          <Button size='sm' className='h-8' onClick={handleApply} disabled={fetchingLogs > 0}>
            {fetchingLogs > 0 ? (
              <Loader2 className='size-3.5 animate-spin' />
            ) : (
              <Search className='size-3.5' />
            )}
            {t('Search')}
          </Button>
          {viewOptions}
        </div>
      </div>
    </div>
    <ExportLogsDialog
      open={exportDialogOpen}
      onOpenChange={setExportDialogOpen}
      defaultStart={filters.startTime}
      defaultEnd={filters.endTime}
      exporting={exporting}
      onConfirm={handleExportConfirm}
      offlineExporting={offlineExporting}
      onConfirmOffline={handleOfflineExportConfirm}
    />
    </>
  )
}
