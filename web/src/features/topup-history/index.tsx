import { useEffect, useMemo, useState } from 'react'
import { Coins, ReceiptText, Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useIsAdmin, useIsStaff } from '@/hooks/use-admin'
import { useDebounce } from '@/hooks/use-debounce'
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
import { DateTimePicker } from '@/components/datetime-picker'
import { InvoiceTicketDialog } from '@/features/tickets/components/invoice-ticket-dialog'
import { adminCompleteTopup, fetchTopups, type TopupListParams } from './api'
import type { TopupRecord, TopupStatus } from './types'

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

const PAGE_SIZES = [10, 20, 50, 100]

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

  const [keyword, setKeyword] = useState('')
  const [status, setStatus] = useState<'all' | TopupStatus>('all')
  const [startDate, setStartDate] = useState<Date | undefined>()
  const [endDate, setEndDate] = useState<Date | undefined>()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(10)
  const [items, setItems] = useState<TopupRecord[]>([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [completingTradeNo, setCompletingTradeNo] = useState<string | null>(
    null
  )
  const [pendingCompleteTradeNo, setPendingCompleteTradeNo] = useState<
    string | null
  >(null)
  const [invoiceDialogOpen, setInvoiceDialogOpen] = useState(false)

  const debouncedKeyword = useDebounce(keyword, 350)

  const params = useMemo<TopupListParams>(
    () => ({
      page,
      pageSize,
      keyword: debouncedKeyword || undefined,
      status: status === 'all' ? undefined : status,
      startTime: startDate ? Math.floor(startDate.getTime() / 1000) : undefined,
      endTime: endDate ? Math.floor(endDate.getTime() / 1000) : undefined,
      admin,
    }),
    [page, pageSize, debouncedKeyword, status, startDate, endDate, admin]
  )

  useEffect(() => {
    let active = true
    setLoading(true)
    fetchTopups(params)
      .then((data) => {
        if (!active) return
        setItems(data.items || [])
        setTotal(data.total || 0)
      })
      .catch((e) => {
        if (!active) return
        toast.error(
          e instanceof Error ? e.message : t('Failed to load topup history')
        )
        setItems([])
        setTotal(0)
      })
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
  }, [params, t])

  // Reset to page 1 when filters change
  useEffect(() => {
    setPage(1)
  }, [debouncedKeyword, status, startDate, endDate, pageSize])

  const totalPages = Math.max(1, Math.ceil(total / pageSize))

  const handleAdminComplete = async (tradeNo: string) => {
    setCompletingTradeNo(tradeNo)
    try {
      await adminCompleteTopup(tradeNo)
      toast.success(t('Order completed'))
      // Refetch
      const data = await fetchTopups(params)
      setItems(data.items || [])
      setTotal(data.total || 0)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to complete order')
      )
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
            onValueChange={(v) => setStatus(v as 'all' | TopupStatus)}
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
            onChange={setStartDate}
            placeholder={t('Start time')}
            className='col-span-2 w-full sm:w-[200px]'
          />
          <DateTimePicker
            value={endDate}
            onChange={setEndDate}
            placeholder={t('End time')}
            className='col-span-2 w-full sm:w-[200px]'
          />
          <div className='relative col-span-2 w-full sm:w-[220px]'>
            <Search className='text-muted-foreground absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2' />
            <Input
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              placeholder={t('Search trade number')}
              className='pl-8'
            />
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

        {/* Pagination */}
        {total > 0 && (
          <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
            <div className='text-muted-foreground text-xs'>
              {t('Total {{n}} records', { n: total })}
            </div>
            <div className='flex flex-wrap items-center gap-2'>
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
