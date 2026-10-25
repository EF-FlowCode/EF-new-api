import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import { Coins, ReceiptText, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import { cn } from '@/lib/utils'
import { useIsAdmin, useIsStaff } from '@/hooks/use-admin'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import {
  ServerPagination,
  useLatestRequest,
  useServerPagination,
} from '@/components/data-table'
import { DateTimePicker } from '@/components/datetime-picker'
import { InvoiceTicketDialog } from '@/features/tickets/components/invoice-ticket-dialog'
import { adminCompleteTopup, fetchTopups, type TopupListParams } from './api'
import type { TopupRecord, TopupStatus } from './types'

const route = getRouteApi('/_authenticated/topup-history/')

const STATUS_OPTIONS: Array<{ value: 'all' | TopupStatus; labelKey: string }> =
  [
    { value: 'all', labelKey: 'All statuses' },
    { value: 'success', labelKey: 'Success' },
    { value: 'pending', labelKey: 'Pending' },
    { value: 'failed', labelKey: 'Failed' },
    { value: 'expired', labelKey: 'Expired' },
  ]

const STATUS_BADGE: Record<string, { label: string; className: string }> = {
  success: {
    label: 'Success',
    className:
      'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  },
  pending: {
    label: 'Pending',
    className:
      'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  },
  failed: {
    label: 'Failed',
    className:
      'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400',
  },
  expired: {
    label: 'Expired',
    className: 'border-muted-foreground/40 bg-muted/40 text-muted-foreground',
  },
}

const PAYMENT_METHOD_LABELS: Record<string, string> = {
  stripe: 'Stripe',
  creem: 'Creem',
  waffo: 'Waffo',
  alipay: 'Alipay',
  wxpay: 'WeChat Pay',
}

function formatTimestamp(unixSec: number | undefined): string {
  if (!unixSec) return '—'
  return new Date(unixSec * 1000).toLocaleString([], {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

export function TopupHistory() {
  const { t } = useTranslation()
  // eflow: 客服 (role>=5) 也走「管理视角」拉全部用户充值记录，但只读——
  //   - admin = useIsStaff 控制 API 走哪条 + UI 是否显示 Username 列
  //   - canCompleteTopup 单独用 useIsAdmin 控制「Complete」按钮（只有 admin 能补单）
  const admin = useIsStaff()
  const canCompleteTopup = useIsAdmin()

  const [items, setItems] = useState<TopupRecord[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const search = route.useSearch()
  const navigate = route.useNavigate()
  const pagination = useServerPagination({
    search,
    navigate,
    total,
    defaultPageSize: 10,
  })
  const { page, pageSize } = pagination
  const runListRequest = useLatestRequest()
  const appliedKeyword = search.filter ?? ''
  const status: 'all' | TopupStatus = search.status ?? 'all'
  const startDate = useMemo(
    () => (search.startTime ? new Date(search.startTime * 1000) : undefined),
    [search.startTime]
  )
  const endDate = useMemo(
    () => (search.endTime ? new Date(search.endTime * 1000) : undefined),
    [search.endTime]
  )
  const [keyword, setKeyword] = useState(appliedKeyword)
  const [completingTradeNo, setCompletingTradeNo] = useState<string | null>(
    null
  )
  const [pendingCompleteTradeNo, setPendingCompleteTradeNo] = useState<
    string | null
  >(null)
  const [invoiceDialogOpen, setInvoiceDialogOpen] = useState(false)

  useEffect(() => setKeyword(appliedKeyword), [appliedKeyword])

  const params = useMemo<TopupListParams>(
    () => ({
      page,
      pageSize,
      keyword: appliedKeyword || undefined,
      status: status === 'all' ? undefined : status,
      startTime: search.startTime,
      endTime: search.endTime,
      admin,
    }),
    [
      page,
      pageSize,
      appliedKeyword,
      status,
      search.startTime,
      search.endTime,
      admin,
    ]
  )
  const paramsRef = useRef(params)
  paramsRef.current = params

  const refresh = useCallback(
    async (requestParams: TopupListParams) => {
      setLoading(true)
      await runListRequest((signal) => fetchTopups(requestParams, signal), {
        onSuccess: (data) => {
          setItems(data.items || [])
          setTotal(data.total || 0)
        },
        onError: (error) => {
          toast.error(
            getRequestErrorMessage(error, t('Failed to load topup history'))
          )
          setItems([])
          setTotal(0)
        },
        onSettled: () => setLoading(false),
      })
    },
    [runListRequest, t]
  )

  useEffect(() => {
    void refresh(params)
  }, [params, refresh])

  const applyKeyword = () => {
    const nextKeyword = keyword.trim()
    pagination.resetPage({ filter: nextKeyword || undefined })
  }

  const handleAdminComplete = async (tradeNo: string) => {
    setCompletingTradeNo(tradeNo)
    try {
      await adminCompleteTopup(tradeNo)
      toast.success(t('Order completed'))
      await refresh(paramsRef.current)
    } catch (e) {
      toast.error(getRequestErrorMessage(e, t('Failed to complete order')))
    } finally {
      setCompletingTradeNo(null)
      setPendingCompleteTradeNo(null)
    }
  }

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-[1440px] space-y-5'>
        <div className='flex flex-col gap-1'>
          <h1 className='text-xl font-semibold tracking-tight'>
            {t('Topup History')}
          </h1>
          <p className='text-muted-foreground text-sm'>
            {admin ? t("All users' topup orders") : t('Your past topup orders')}
          </p>
        </div>

        {/* Filter bar */}
        <div className='bg-card grid grid-cols-2 gap-2 rounded-xl border p-3 sm:flex sm:flex-wrap sm:items-center'>
          {/* eflow: 原本写 {!admin && …} 把站长 admin 角色也藏了，但
              InvoiceTicketDialog 内部其实只拉当前用户自己的可开票订单，
              admin 也该能看到入口（站长本人开票走的就是这条路）。 */}
          <Button
            type='button'
            variant='default'
            onClick={() => setInvoiceDialogOpen(true)}
            className='w-full sm:w-auto'
          >
            <ReceiptText className='mr-1.5 h-3.5 w-3.5' />
            {t('Request invoice')}
          </Button>
          <Select
            value={status}
            onValueChange={(value) =>
              pagination.resetPage({
                status: value === 'all' ? undefined : value,
              })
            }
          >
            <SelectTrigger className='w-full sm:w-[140px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {STATUS_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={opt.value}>
                  {t(opt.labelKey)}
                </SelectItem>
              ))}
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
          <div className='relative col-span-2 flex w-full items-center gap-2 sm:w-[300px]'>
            <Search className='text-muted-foreground absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2' />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  applyKeyword()
                }
              }}
              placeholder={t('Search trade number')}
              className='min-w-0 flex-1 pl-8'
            />
            <Button
              variant='outline'
              size='sm'
              className='h-9 shrink-0 sm:h-8'
              onClick={applyKeyword}
            >
              <Search className='size-3.5' />
              {t('Search')}
            </Button>
          </div>
        </div>

        {/* Mobile records */}
        <div className='bg-card divide-y overflow-hidden rounded-xl border sm:hidden'>
          {loading && items.length === 0 ? (
            Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className='space-y-3 p-4'>
                <Skeleton className='h-4 w-2/3' />
                <Skeleton className='h-12 w-full' />
              </div>
            ))
          ) : items.length === 0 ? (
            <div className='text-muted-foreground p-12 text-center text-sm'>
              {t('No topup records')}
            </div>
          ) : (
            items.map((record) => {
              const badge = STATUS_BADGE[record.status] ?? {
                label: record.status,
                className:
                  'border-muted-foreground/40 bg-muted/40 text-muted-foreground',
              }
              const paymentLabel =
                record.payment_method &&
                PAYMENT_METHOD_LABELS[record.payment_method]
                  ? PAYMENT_METHOD_LABELS[record.payment_method]
                  : record.payment_method || '—'
              return (
                <article key={record.id} className='space-y-3 p-4'>
                  <div className='flex items-start justify-between gap-3'>
                    <span className='min-w-0 font-mono text-xs break-all'>
                      {record.trade_no}
                    </span>
                    <Badge
                      variant='outline'
                      className={cn('shrink-0 font-normal', badge.className)}
                    >
                      {t(badge.label)}
                    </Badge>
                  </div>
                  {admin && (record.username || record.user_id) && (
                    <div className='text-muted-foreground text-xs'>
                      {t('User')}: {record.username || `#${record.user_id}`}
                    </div>
                  )}
                  <div className='grid grid-cols-2 gap-x-4 gap-y-3 text-sm'>
                    <div>
                      <div className='text-muted-foreground text-xs'>
                        {t('Payment')}
                      </div>
                      <div className='mt-0.5'>{paymentLabel}</div>
                    </div>
                    <div>
                      <div className='text-muted-foreground text-xs'>
                        {t('Created')}
                      </div>
                      <div className='mt-0.5 text-xs'>
                        {formatTimestamp(record.create_time)}
                      </div>
                    </div>
                    <div>
                      <div className='text-muted-foreground text-xs'>
                        {t('Quota')}
                      </div>
                      <div className='mt-0.5 inline-flex items-center gap-1 font-mono'>
                        <Coins className='text-muted-foreground/70 h-3.5 w-3.5' />
                        {record.amount}
                      </div>
                    </div>
                    <div>
                      <div className='text-muted-foreground text-xs'>
                        {t('Amount')}
                      </div>
                      <div className='mt-0.5 text-rose-600 dark:text-rose-400'>
                        ¥{(record.money ?? 0).toFixed(2)}
                      </div>
                    </div>
                  </div>
                  {canCompleteTopup && record.status === 'pending' && (
                    <Button
                      size='sm'
                      variant='outline'
                      className='w-full'
                      disabled={completingTradeNo === record.trade_no}
                      onClick={() => setPendingCompleteTradeNo(record.trade_no)}
                    >
                      {t('Complete')}
                    </Button>
                  )}
                </article>
              )
            })
          )}
        </div>

        {/* Table */}
        <div className='bg-card hidden overflow-x-auto rounded-xl border sm:block'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Trade no.')}</TableHead>
                {admin && <TableHead className='w-28'>{t('User')}</TableHead>}
                <TableHead className='w-32'>{t('Payment')}</TableHead>
                <TableHead className='w-32'>{t('Quota')}</TableHead>
                <TableHead className='w-28'>{t('Amount')}</TableHead>
                <TableHead className='w-28'>{t('Status')}</TableHead>
                {admin && (
                  <TableHead className='w-24'>{t('Actions')}</TableHead>
                )}
                <TableHead className='w-44'>{t('Created')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading && items.length === 0 ? (
                Array.from({ length: 5 }).map((_, i) => (
                  <TableRow key={i}>
                    {Array.from({ length: admin ? 8 : 6 }).map((_, j) => (
                      <TableCell key={j}>
                        <Skeleton className='h-4 w-full' />
                      </TableCell>
                    ))}
                  </TableRow>
                ))
              ) : items.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={admin ? 8 : 6}
                    className='text-muted-foreground py-12 text-center text-sm'
                  >
                    {t('No topup records')}
                  </TableCell>
                </TableRow>
              ) : (
                items.map((record) => {
                  const badge = STATUS_BADGE[record.status] ?? {
                    label: record.status,
                    className:
                      'border-muted-foreground/40 bg-muted/40 text-muted-foreground',
                  }
                  const paymentLabel =
                    record.payment_method &&
                    PAYMENT_METHOD_LABELS[record.payment_method]
                      ? PAYMENT_METHOD_LABELS[record.payment_method]
                      : record.payment_method || '—'
                  return (
                    <TableRow key={record.id}>
                      <TableCell className='font-mono text-xs'>
                        {record.trade_no}
                      </TableCell>
                      {admin && (
                        <TableCell className='font-mono text-xs'>
                          {record.username ||
                            (record.user_id ? `#${record.user_id}` : '—')}
                        </TableCell>
                      )}
                      <TableCell>{paymentLabel}</TableCell>
                      <TableCell>
                        <span className='inline-flex items-center gap-1'>
                          <Coins className='text-muted-foreground/70 h-3.5 w-3.5' />
                          <span className='font-mono'>{record.amount}</span>
                        </span>
                      </TableCell>
                      <TableCell className='text-rose-600 dark:text-rose-400'>
                        ¥{(record.money ?? 0).toFixed(2)}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant='outline'
                          className={cn('font-normal', badge.className)}
                        >
                          {t(badge.label)}
                        </Badge>
                      </TableCell>
                      {admin && (
                        <TableCell>
                          {canCompleteTopup && record.status === 'pending' && (
                            <Button
                              size='sm'
                              variant='outline'
                              disabled={completingTradeNo === record.trade_no}
                              onClick={() =>
                                setPendingCompleteTradeNo(record.trade_no)
                              }
                            >
                              {t('Complete')}
                            </Button>
                          )}
                        </TableCell>
                      )}
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatTimestamp(record.create_time)}
                      </TableCell>
                    </TableRow>
                  )
                })
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

      <InvoiceTicketDialog
        open={invoiceDialogOpen}
        onOpenChange={setInvoiceDialogOpen}
      />

      <AlertDialog
        open={pendingCompleteTradeNo !== null}
        onOpenChange={(open) => {
          if (!open) setPendingCompleteTradeNo(null)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Complete the order?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'This will mark the order as success and credit the quota to the user.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (pendingCompleteTradeNo) {
                  handleAdminComplete(pendingCompleteTradeNo)
                }
              }}
            >
              {t('Confirm')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
