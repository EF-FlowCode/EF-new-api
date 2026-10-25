import { useCallback, useEffect, useMemo, useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  ServerPagination,
  useLatestRequest,
  useServerPagination,
} from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import {
  fetchAuditLogs,
  type AuditListParams,
  type AuditLog,
  type AuditScope,
} from './api'
import {
  AUDIT_CATEGORIES,
  auditActionLabel,
  auditAuthMethodLabel,
  auditCategoryLabel,
  loginMethodLabel,
} from './lib/audit-labels'

const route = getRouteApi('/_authenticated/audit-logs/')

function formatTimestamp(unixSec: number | undefined): string {
  if (!unixSec) return '—'
  return new Date(unixSec * 1000).toLocaleString([], {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

function ResultBadge({ success }: { success: boolean }) {
  const { t } = useTranslation()
  return (
    <Badge
      variant='outline'
      className={cn(
        'font-normal',
        success
          ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
          : 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
      )}
    >
      {success ? t('Success') : t('Failed')}
    </Badge>
  )
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <div className='grid grid-cols-[8rem_1fr] gap-3 text-sm'>
      <div className='text-muted-foreground'>{label}</div>
      <div className='min-w-0 font-mono text-xs break-all'>{value || '—'}</div>
    </div>
  )
}

function AuditLogDetailsDialog({
  log,
  onOpenChange,
}: {
  log: AuditLog | null
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  if (!log) return null
  const params = log.other?.op?.params
  const admin = log.other?.admin_info
  const request = log.other?.audit_info
  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{auditActionLabel(log, t)}</DialogTitle>
        </DialogHeader>
        <div className='space-y-2'>
          <DetailRow
            label={t('Time')}
            value={formatTimestamp(log.created_at)}
          />
          <DetailRow
            label={t('User')}
            value={`${log.username || '—'} (#${log.user_id})`}
          />
          <DetailRow
            label={t('Category')}
            value={auditCategoryLabel(log.category, t)}
          />
          <DetailRow
            label={t('Result')}
            value={`${log.success ? t('Success') : t('Failed')} · HTTP ${log.status}`}
          />
          <DetailRow
            label={t('Authentication')}
            value={auditAuthMethodLabel(log.auth_method, t)}
          />
          {log.other?.login_method && (
            <DetailRow
              label={t('Sign-in method')}
              value={loginMethodLabel(log.other.login_method, t)}
            />
          )}
          <DetailRow label='IP' value={log.ip} />
          <DetailRow
            label={t('Request')}
            value={`${log.method} ${log.route}`.trim()}
          />
          <DetailRow label={t('Request ID')} value={log.request_id} />
          {log.token_ref && (
            <DetailRow label={t('Token reference')} value={log.token_ref} />
          )}
          <DetailRow label='User-Agent' value={log.user_agent} />
          {admin && (
            <DetailRow
              label={t('Operator')}
              value={`${admin.admin_username || '—'} (#${admin.admin_id ?? 0})`}
            />
          )}
          {request?.params && Object.keys(request.params).length > 0 && (
            <DetailRow
              label={t('Route parameters')}
              value={JSON.stringify(request.params)}
            />
          )}
          {params && Object.keys(params).length > 0 && (
            <DetailRow label={t('Details')} value={JSON.stringify(params)} />
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}

function UsernameFilter({
  initialValue,
  onApply,
}: {
  initialValue: string
  onApply: (value: string) => void
}) {
  const { t } = useTranslation()
  const [value, setValue] = useState(initialValue)
  return (
    <div className='relative col-span-2 flex w-full items-center gap-2 sm:w-[280px]'>
      <Search className='text-muted-foreground absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2' />
      <Input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            onApply(value.trim())
          }
        }}
        placeholder={t('Exact username')}
        className='min-w-0 flex-1 pl-8'
      />
      <Button
        variant='outline'
        size='sm'
        className='h-9 shrink-0 sm:h-8'
        onClick={() => onApply(value.trim())}
      >
        {t('Search')}
      </Button>
    </div>
  )
}

export function AuditLogs() {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const scope: AuditScope = isAdmin && search.scope !== 'self' ? 'all' : 'self'
  const [items, setItems] = useState<AuditLog[]>([])
  const [total, setTotal] = useState(0)
  const [settledKey, setSettledKey] = useState<string | null>(null)
  const [selected, setSelected] = useState<AuditLog | null>(null)
  const pagination = useServerPagination({
    search,
    navigate,
    total,
    defaultPageSize: 20,
  })
  const { page, pageSize } = pagination
  const runListRequest = useLatestRequest()

  const params = useMemo<AuditListParams>(
    () => ({
      page,
      pageSize,
      category: search.category,
      success:
        search.result === undefined ? undefined : search.result === 'success',
      username: search.username,
      startTime: search.startTime,
      endTime: search.endTime,
    }),
    [
      page,
      pageSize,
      search.category,
      search.result,
      search.username,
      search.startTime,
      search.endTime,
    ]
  )

  // Loading is derived from the request key so the effect never sets state synchronously.
  const requestKey = JSON.stringify([scope, params])
  const loading = settledKey !== requestKey

  const refresh = useCallback(
    async (
      requestScope: AuditScope,
      requestParams: AuditListParams,
      key: string
    ) => {
      await runListRequest(
        (signal) => fetchAuditLogs(requestScope, requestParams, signal),
        {
          onSuccess: (data) => {
            setItems(data.items || [])
            setTotal(data.total || 0)
          },
          onError: (error) => {
            toast.error(
              getRequestErrorMessage(error, t('Failed to load audit records'))
            )
            setItems([])
            setTotal(0)
          },
          onSettled: () => setSettledKey(key),
        }
      )
    },
    [runListRequest, t]
  )

  useEffect(() => {
    void refresh(scope, params, JSON.stringify([scope, params]))
  }, [scope, params, refresh])

  const startDate = search.startTime
    ? new Date(search.startTime * 1000)
    : undefined
  const endDate = search.endTime ? new Date(search.endTime * 1000) : undefined
  const showUser = scope === 'all'
  const columnCount = showUser ? 7 : 6

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-[1440px] space-y-5'>
        <div className='flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between'>
          <div className='flex flex-col gap-1'>
            <h1 className='text-xl font-semibold tracking-tight'>
              {t('Audit Logs')}
            </h1>
            <p className='text-muted-foreground text-sm'>
              {t(
                'Sign-ins, security changes, administrative writes and access token calls. Records are kept permanently.'
              )}
            </p>
          </div>
          {isAdmin && (
            <Tabs
              value={scope}
              onValueChange={(value) =>
                pagination.resetPage({
                  scope: value === 'self' ? 'self' : undefined,
                  username: undefined,
                })
              }
            >
              <TabsList aria-label={t('View scope')}>
                <TabsTrigger value='all'>{t('All')}</TabsTrigger>
                <TabsTrigger value='self'>{t('Only Mine')}</TabsTrigger>
              </TabsList>
            </Tabs>
          )}
        </div>

        <div className='bg-card grid grid-cols-2 gap-2 rounded-xl border p-3 sm:flex sm:flex-wrap sm:items-center'>
          <Select
            value={search.category ?? 'all'}
            onValueChange={(value) =>
              pagination.resetPage({
                category: value === 'all' ? undefined : value,
              })
            }
          >
            <SelectTrigger className='w-full sm:w-[160px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All categories')}</SelectItem>
              {AUDIT_CATEGORIES.map((category) => (
                <SelectItem key={category} value={category}>
                  {auditCategoryLabel(category, t)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select
            value={search.result ?? 'all'}
            onValueChange={(value) =>
              pagination.resetPage({
                result: value === 'all' ? undefined : value,
              })
            }
          >
            <SelectTrigger className='w-full sm:w-[140px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='all'>{t('All results')}</SelectItem>
              <SelectItem value='success'>{t('Success')}</SelectItem>
              <SelectItem value='failed'>{t('Failed')}</SelectItem>
            </SelectContent>
          </Select>
          <DateTimePicker
            value={startDate}
            onChange={(value) =>
              pagination.resetPage({
                startTime: value
                  ? Math.floor(value.getTime() / 1000)
                  : undefined,
              })
            }
            placeholder={t('Start time')}
            className='col-span-2 w-full sm:w-[200px]'
          />
          <DateTimePicker
            value={endDate}
            onChange={(value) =>
              pagination.resetPage({
                endTime: value ? Math.floor(value.getTime() / 1000) : undefined,
              })
            }
            placeholder={t('End time')}
            className='col-span-2 w-full sm:w-[200px]'
          />
          {showUser && (
            <UsernameFilter
              key={search.username ?? ''}
              initialValue={search.username ?? ''}
              onApply={(value) =>
                pagination.resetPage({ username: value || undefined })
              }
            />
          )}
        </div>

        <div className='bg-card overflow-x-auto rounded-xl border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className='w-44'>{t('Time')}</TableHead>
                {showUser && (
                  <TableHead className='w-32'>{t('User')}</TableHead>
                )}
                <TableHead className='w-32'>{t('Category')}</TableHead>
                <TableHead>{t('Event')}</TableHead>
                <TableHead className='w-24'>{t('Result')}</TableHead>
                <TableHead className='w-32'>{t('Authentication')}</TableHead>
                <TableHead className='w-36'>IP</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && items.length === 0 ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: columnCount }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className='h-4 w-full' />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={columnCount}
                    className='text-muted-foreground py-12 text-center text-sm'
                  >
                    {t('No audit records')}
                  </TableCell>
                </TableRow>
              ) : (
                items.map((log) => (
                  <TableRow
                    key={log.event_id}
                    className='cursor-pointer'
                    onClick={() => setSelected(log)}
                  >
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTimestamp(log.created_at)}
                    </TableCell>
                    {showUser && (
                      <TableCell className='font-mono text-xs'>
                        {log.username || `#${log.user_id}`}
                      </TableCell>
                    )}
                    <TableCell className='text-sm'>
                      {auditCategoryLabel(log.category, t)}
                    </TableCell>
                    <TableCell className='text-sm'>
                      {auditActionLabel(log, t)}
                    </TableCell>
                    <TableCell>
                      <ResultBadge success={log.success} />
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {auditAuthMethodLabel(log.auth_method, t)}
                    </TableCell>
                    <TableCell className='font-mono text-xs'>
                      {log.ip || '—'}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        <ServerPagination
          table={pagination.table}
          total={total}
          loading={loading}
        />
      </div>

      <AuditLogDetailsDialog
        log={selected}
        onOpenChange={(open) => {
          if (!open) setSelected(null)
        }}
      />
    </div>
  )
}
