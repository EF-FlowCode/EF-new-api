import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Plus, RefreshCw, XCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { useLatestRequest } from '@/components/data-table/latest-request'
import {
  batchAdminUpdateTicketStatus,
  fetchTickets,
  exportTicketInvoices,
  type TicketSortField,
} from '../api'
import { TICKET_STATUS_META } from '../lib/ticket-utils'
import { TICKET_STATUS, type Ticket, type TicketStatus } from '../types'
import { CreateTicketDialog } from './create-ticket-dialog'
import { TicketsTable } from './tickets-table'

const PAGE_SIZES = [10, 20, 50, 100]

const TYPE_OPTIONS = [
  { value: 'all', labelKey: 'All types' },
  { value: 'general', labelKey: 'General' },
  { value: 'refund', labelKey: 'Refund' },
  { value: 'invoice', labelKey: 'Invoice' },
]

// ── eflow: 列表视图偏好（排序/分页/筛选）按页面持久化到 localStorage ──
// 排序那版私改只把状态放 useState，离开再进来就重置；这里把整页视图状态记下来，
// 按 detailPathPrefix 分桶（/admin-tickets 与 /tickets 各记各的）。页码不记，
// 进来一律回第 1 页（数据会变，停在旧页更难受）。
type TicketListPrefs = {
  pageSize: number
  statusFilter: 'all' | TicketStatus
  typeFilter: string
  sortField: TicketSortField | null
  sortDir: 'asc' | 'desc'
}

const DEFAULT_PREFS: TicketListPrefs = {
  pageSize: 10,
  statusFilter: 'all',
  typeFilter: 'all',
  sortField: null,
  sortDir: 'desc',
}

const PREFS_VERSION = 1
const prefsKey = (prefix: string) =>
  `eflow:ticket-list-prefs:v${PREFS_VERSION}:${prefix}`

const SORT_FIELDS: TicketSortField[] = [
  'id',
  'created_time',
  'updated_time',
  'status',
  'priority',
]
const VALID_STATUSES = [
  TICKET_STATUS.PENDING,
  TICKET_STATUS.IN_PROGRESS,
  TICKET_STATUS.RESOLVED,
  TICKET_STATUS.CLOSED,
] as number[]
const TYPE_VALUES = TYPE_OPTIONS.map((o) => o.value)

// 逐字段校验，脏数据/旧 schema 一律回退默认，绝不把非法值塞给后端
function sanitizePrefs(p: Partial<TicketListPrefs>): TicketListPrefs {
  const out: TicketListPrefs = { ...DEFAULT_PREFS }
  if (typeof p.pageSize === 'number' && PAGE_SIZES.includes(p.pageSize))
    out.pageSize = p.pageSize
  if (
    p.statusFilter === 'all' ||
    (typeof p.statusFilter === 'number' &&
      VALID_STATUSES.includes(p.statusFilter))
  )
    out.statusFilter = p.statusFilter
  if (typeof p.typeFilter === 'string' && TYPE_VALUES.includes(p.typeFilter))
    out.typeFilter = p.typeFilter
  if (
    p.sortField === null ||
    (typeof p.sortField === 'string' &&
      SORT_FIELDS.includes(p.sortField as TicketSortField))
  )
    out.sortField = (p.sortField ?? null) as TicketSortField | null
  if (p.sortDir === 'asc' || p.sortDir === 'desc') out.sortDir = p.sortDir
  return out
}

function loadPrefs(prefix: string): TicketListPrefs {
  if (typeof window === 'undefined') return { ...DEFAULT_PREFS }
  try {
    const raw = window.localStorage.getItem(prefsKey(prefix))
    if (!raw) return { ...DEFAULT_PREFS }
    const parsed = JSON.parse(raw)
    return sanitizePrefs(parsed && typeof parsed === 'object' ? parsed : {})
  } catch {
    return { ...DEFAULT_PREFS }
  }
}

type Props = {
  admin: boolean
  /** Where to navigate when a row is clicked. */
  detailPathPrefix: string
  title: string
  description: string
  /** Whether to show "New ticket" button. Customer support page hides it. */
  showCreate?: boolean
}

export function TicketListPage({
  admin,
  detailPathPrefix,
  title,
  description,
  showCreate = true,
}: Props) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const canExport =
    useAuthStore((state) => (state.auth.user?.role ?? 0) >= ROLE.ADMIN) && admin
  const runLatest = useLatestRequest()

  // 进页面时一次性把上次的视图偏好读出来当初值（detailPathPrefix 每路由稳定，组件随路由重挂）
  const [initialPrefs] = useState(() => loadPrefs(detailPathPrefix))

  const [tickets, setTickets] = useState<Ticket[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(initialPrefs.pageSize)
  const [statusFilter, setStatusFilter] = useState<'all' | TicketStatus>(
    initialPrefs.statusFilter
  )
  const [typeFilter, setTypeFilter] = useState<string>(initialPrefs.typeFilter)
  const [createOpen, setCreateOpen] = useState(false)
  const [companyNameInput, setCompanyNameInput] = useState('')
  const [companyName, setCompanyName] = useState('')
  const [exporting, setExporting] = useState(false)
  // sortField=null 表示不传排序参数，后端按历史默认（更新时间倒序）返回
  const [sortField, setSortField] = useState<TicketSortField | null>(
    initialPrefs.sortField
  )
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>(initialPrefs.sortDir)

  // 视图偏好变化即写回 localStorage；不可用（隐私模式/配额）时静默降级
  useEffect(() => {
    if (typeof window === 'undefined') return
    try {
      const prefs: TicketListPrefs = {
        pageSize,
        statusFilter,
        typeFilter,
        sortField,
        sortDir,
      }
      window.localStorage.setItem(
        prefsKey(detailPathPrefix),
        JSON.stringify(prefs)
      )
    } catch {
      /* localStorage 写失败不影响列表功能 */
    }
  }, [detailPathPrefix, pageSize, statusFilter, typeFilter, sortField, sortDir])

  // 批量选择（仅管理/客服侧）。跨页保留，便于翻页累积后一次性关闭。
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [confirmCloseOpen, setConfirmCloseOpen] = useState(false)
  const [bulkBusy, setBulkBusy] = useState(false)

  const toggleSelect = (id: number) =>
    setSelectedIds((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const toggleSelectAllOnPage = (checked: boolean) =>
    setSelectedIds((prev) => {
      const next = new Set(prev)
      for (const tk of tickets) {
        if (checked) next.add(tk.id)
        else next.delete(tk.id)
      }
      return next
    })

  const clearSelection = () => setSelectedIds(new Set())

  const handleInvoiceExport = async () => {
    if (exporting || !canExport || selectedIds.size === 0) return
    if (selectedIds.size > 500) {
      toast.error(t('Select between 1 and 500 invoice tickets'))
      return
    }
    setExporting(true)
    try {
      const { blob, filename } = await exportTicketInvoices(
        Array.from(selectedIds)
      )
      const url = URL.createObjectURL(blob)
      const link = document.createElement('a')
      link.href = url
      link.download = filename
      document.body.appendChild(link)
      link.click()
      link.remove()
      setTimeout(() => URL.revokeObjectURL(url), 1000)
    } catch (error) {
      toast.error(
        t(error instanceof Error ? error.message : 'Failed to export invoices')
      )
    } finally {
      setExporting(false)
    }
  }

  const handleBulkClose = async () => {
    const ids = Array.from(selectedIds)
    if (ids.length === 0) return
    setBulkBusy(true)
    try {
      const res = await batchAdminUpdateTicketStatus(ids, TICKET_STATUS.CLOSED)
      if (res.failed > 0) {
        toast.warning(
          t('Closed {{ok}}, {{fail}} failed', {
            ok: res.updated,
            fail: res.failed,
          })
        )
      } else {
        toast.success(t('Closed {{ok}} tickets', { ok: res.updated }))
      }
      clearSelection()
      await refresh()
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to update ticket status')
      )
    } finally {
      setBulkBusy(false)
      setConfirmCloseOpen(false)
    }
  }

  const handleSort = (field: TicketSortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(field)
      setSortDir('desc')
    }
  }

  const params = useMemo(
    () => ({
      page,
      pageSize,
      status: statusFilter === 'all' ? undefined : statusFilter,
      type: typeFilter === 'all' ? undefined : typeFilter,
      admin,
      orderBy: sortField ?? undefined,
      order: sortDir,
      companyName: admin && typeFilter === 'invoice' ? companyName : undefined,
    }),
    [
      page,
      pageSize,
      statusFilter,
      typeFilter,
      admin,
      sortField,
      sortDir,
      companyName,
    ]
  )

  const refresh = useCallback(async () => {
    setLoading(true)
    await runLatest((signal) => fetchTickets(params, signal), {
      onSuccess: (data) => {
        setTickets(data.items || [])
        setTotal(data.total || 0)
      },
      onError: (error) => {
        toast.error(
          error instanceof Error ? error.message : t('Failed to load tickets')
        )
        setTickets([])
        setTotal(0)
      },
      onSettled: () => setLoading(false),
    })
  }, [params, runLatest, t])

  useEffect(() => {
    refresh()
  }, [refresh])

  useEffect(() => {
    setPage(1)
    // 过滤/排序/每页条数变了，原选择已无意义，清空避免误关不在视野里的工单
    clearSelection()
  }, [statusFilter, typeFilter, pageSize, sortField, sortDir, companyName])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-[1440px] space-y-5'>
        <div className='flex flex-col gap-1'>
          <h1 className='text-xl font-semibold tracking-tight'>{title}</h1>
          <p className='text-muted-foreground text-sm'>{description}</p>
        </div>

        <div className='bg-card flex flex-wrap items-center gap-2 rounded-xl border p-3'>
          {showCreate && (
            <Button onClick={() => setCreateOpen(true)}>
              <Plus className='mr-1.5 h-3.5 w-3.5' />
              {t('New ticket')}
            </Button>
          )}
          <Button variant='outline' onClick={refresh}>
            <RefreshCw
              className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
          <Select
            value={String(statusFilter)}
            onValueChange={(v) =>
              setStatusFilter(v === 'all' ? 'all' : (Number(v) as TicketStatus))
            }
          >
            <SelectTrigger className='w-[160px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All statuses')}</SelectItem>
              {(
                [
                  TICKET_STATUS.PENDING,
                  TICKET_STATUS.IN_PROGRESS,
                  TICKET_STATUS.RESOLVED,
                  TICKET_STATUS.CLOSED,
                ] as const
              ).map((s) => (
                <SelectItem key={s} value={String(s)}>
                  {t(TICKET_STATUS_META[s].labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={typeFilter}
            onValueChange={(value) => {
              setTypeFilter(value)
              if (value !== 'invoice') {
                setCompanyName('')
                setCompanyNameInput('')
              }
            }}
          >
            <SelectTrigger className='w-[160px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TYPE_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {t(opt.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {admin && (
          <form
            className='flex flex-wrap items-end gap-2'
            onSubmit={(event) => {
              event.preventDefault()
              setCompanyName(companyNameInput.trim())
              setTypeFilter('invoice')
              setPage(1)
              clearSelection()
            }}
          >
            <div className='flex min-w-48 flex-1 flex-col gap-2 sm:max-w-sm'>
              <Label htmlFor='invoice-company-search'>
                {t('Search invoice title')}
              </Label>
              <Input
                id='invoice-company-search'
                value={companyNameInput}
                placeholder={t('Enter part of a company or invoice title')}
                maxLength={255}
                onChange={(event) => setCompanyNameInput(event.target.value)}
              />
            </div>
            <Button type='submit' variant='outline'>
              {t('Search')}
            </Button>
            {companyName && (
              <Button
                type='button'
                variant='ghost'
                onClick={() => {
                  setCompanyName('')
                  setCompanyNameInput('')
                  setPage(1)
                  clearSelection()
                }}
              >
                {t('Clear search')}
              </Button>
            )}
          </form>
        )}

        {admin && selectedIds.size > 0 && (
          <div className='bg-card flex flex-wrap items-center gap-3 rounded-xl border border-dashed p-3'>
            <span className='text-sm font-medium'>
              {t('{{n}} selected', { n: selectedIds.size })}
            </span>
            {canExport && typeFilter === 'invoice' && (
              <Button
                size='sm'
                variant='outline'
                disabled={exporting || bulkBusy || loading}
                onClick={handleInvoiceExport}
              >
                {exporting
                  ? t('Exporting invoices...')
                  : t('Export selected invoices')}
              </Button>
            )}
            <Button
              size='sm'
              variant='destructive'
              disabled={bulkBusy}
              onClick={() => setConfirmCloseOpen(true)}
            >
              {t('Mark as closed')}
            </Button>
            <Button
              size='sm'
              variant='ghost'
              disabled={bulkBusy}
              onClick={clearSelection}
            >
              <XCircle className='mr-1.5 h-3.5 w-3.5' />
              {t('Clear selection')}
            </Button>
          </div>
        )}

        <TicketsTable
          tickets={tickets}
          loading={loading}
          showAssignee={admin}
          sortField={sortField}
          sortDir={sortDir}
          onSort={handleSort}
          selectable={admin}
          selectedIds={selectedIds}
          onToggleSelect={toggleSelect}
          onToggleSelectAll={toggleSelectAllOnPage}
          onRowClick={(ticket) =>
            navigate({ to: `${detailPathPrefix}/${ticket.id}` })
          }
        />

        {total > 0 && (
          <div className='flex flex-wrap items-center justify-between gap-3'>
            <div className='text-muted-foreground text-xs'>
              {t('Total {{n}} records', { n: total })}
            </div>
            <div className='flex items-center gap-2'>
              <Select
                value={String(pageSize)}
                onValueChange={(v) => setPageSize(Number(v))}
              >
                <SelectTrigger className='w-[100px]'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PAGE_SIZES.map((size) => (
                    <SelectItem key={size} value={String(size)}>
                      {t('{{n}} / page', { n: size })}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page <= 1 || loading}
                onClick={() => setPage((p) => Math.max(1, p - 1))}
              >
                {t('Previous')}
              </Button>
              <span className='text-muted-foreground text-xs'>
                {t('Page {{p}} / {{total}}', { p: page, total: totalPages })}
              </span>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={page >= totalPages || loading}
                onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              >
                {t('Next')}
              </Button>
            </div>
          </div>
        )}
      </div>

      <CreateTicketDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={refresh}
      />

      <AlertDialog
        open={confirmCloseOpen}
        onOpenChange={(o) => !bulkBusy && setConfirmCloseOpen(o)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Mark as closed')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'Close {{n}} selected tickets? This will notify their users.',
                {
                  n: selectedIds.size,
                }
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={bulkBusy}>
              {t('Cancel')}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={bulkBusy}
              onClick={(e) => {
                e.preventDefault()
                handleBulkClose()
              }}
            >
              {t('Mark as closed')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
