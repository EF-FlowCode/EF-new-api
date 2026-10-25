import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { ChevronLeft, ChevronRight, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { formatLogQuota } from '@/lib/format'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { getPersonalRefundRecords, type RefundRecordKind } from '../../api'

export function PersonalRefundDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [kind, setKind] = useState<RefundRecordKind>('usage')
  const [page, setPage] = useState(1)
  const records = useQuery({
    queryKey: ['personal-refunds', userId, kind, page],
    meta: { handlesServerError: true },
    queryFn: ({ signal }) => getPersonalRefundRecords(kind, page, signal),
    enabled: open && !!userId,
    retry: false,
    staleTime: 0,
  })
  const statuses = {
    completed: 'Completed',
    pending: 'Pending',
    rejected: 'Rejected',
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[85dvh] grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('Refunds and returns')}</DialogTitle>
          <DialogDescription>
            {t(
              'Consumption returns, automatic refunds and refund tickets are recorded separately to avoid counting the same credit twice.'
            )}
          </DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-2'>
          <Tabs
            value={kind}
            onValueChange={(value) => {
              setKind(value as RefundRecordKind)
              setPage(1)
            }}
          >
            <TabsList className='flex h-auto flex-wrap'>
              <TabsTrigger value='usage'>
                {t('Consumption returned')}
              </TabsTrigger>
              <TabsTrigger value='task'>{t('Automatic refunds')}</TabsTrigger>
              <TabsTrigger value='ticket'>{t('Refund tickets')}</TabsTrigger>
            </TabsList>
          </Tabs>
          {kind === 'ticket' && (
            <p className='text-muted-foreground text-xs leading-5'>
              {t(
                'Ticket amounts are requested quota, not confirmed external payments. They do not reduce consumption.'
              )}
            </p>
          )}
        </div>
        <div className='min-h-32 overflow-y-auto'>
          {records.isPending ? (
            <Skeleton className='h-32 w-full' />
          ) : records.isError ? (
            <div
              role='alert'
              className='flex flex-wrap items-center gap-2 text-sm'
            >
              <span>{t('Failed to load refund records')}</span>
              <Button
                variant='outline'
                size='sm'
                onClick={() => void records.refetch()}
              >
                <RefreshCw data-icon='inline-start' />
                {t('Retry')}
              </Button>
            </div>
          ) : !records.data?.items.length ? (
            <p className='text-muted-foreground py-8 text-center text-sm'>
              {t('No records found')}
            </p>
          ) : (
            <ul className='divide-y'>
              {records.data.items.map((item) => (
                <li key={item.id} className='flex flex-col gap-2 py-3'>
                  <div className='flex flex-wrap items-center justify-between gap-2 text-sm'>
                    <time>
                      {new Date(item.created_at * 1000).toLocaleString()}
                    </time>
                    <strong className='break-all tabular-nums'>
                      {kind === 'ticket'
                        ? `${t('Requested refund quota')}: `
                        : '+'}
                      {formatLogQuota(item.quota)}
                    </strong>
                  </div>
                  <div className='flex items-start gap-2'>
                    <Badge variant='secondary'>
                      {t(statuses[item.status])}
                    </Badge>
                    <p className='min-w-0 text-sm break-words'>
                      {item.reason || t('Automatic refunds')}
                    </p>
                  </div>
                  <p className='text-muted-foreground text-xs break-all'>
                    {kind === 'usage'
                      ? `${t('Refund batch')}: ${item.id}`
                      : kind === 'ticket'
                        ? `${t('Tickets')}: #${item.ticket_id}`
                        : `${t('Log ID')}: ${item.id}`}
                    {item.request_id && ` · ${item.request_id}`}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className='flex items-center justify-end gap-3'>
          <Button
            variant='outline'
            size='icon-sm'
            aria-label={t('Previous page')}
            disabled={page === 1 || records.isFetching}
            onClick={() => setPage(page - 1)}
          >
            <ChevronLeft />
          </Button>
          <span className='text-muted-foreground text-sm'>{page}</span>
          <Button
            variant='outline'
            size='icon-sm'
            aria-label={t('Next page')}
            disabled={
              !records.data?.has_more || records.isFetching || records.isError
            }
            onClick={() => setPage(page + 1)}
          >
            <ChevronRight />
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
