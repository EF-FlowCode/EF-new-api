import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { CreditCard, History, Info, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { useSystemConfigStore } from '@/stores/system-config-store'
import {
  formatQuotaWithCurrency,
  getCurrencyLabel,
  isCurrencyDisplayEnabled,
} from '@/lib/currency'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { getAccountSummary } from '../../api'
import { useSummaryCardsConfig } from '../../hooks/use-dashboard-config'
import { PersonalRefundDialog } from './personal-refund-dialog'

const formatAmount = (quota: number) =>
  formatQuotaWithCurrency(quota, {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
  })

export function SummaryCards() {
  const { t } = useTranslation()
  useSystemConfigStore((state) => state.config.currency)
  const userId = useAuthStore((state) => state.auth.user?.id)
  const [historyOpen, setHistoryOpen] = useState(false)
  const summary = useQuery({
    queryKey: ['account-summary', userId],
    meta: { handlesServerError: true },
    queryFn: ({ signal }) => getAccountSummary(signal),
    enabled: !!userId,
    staleTime: 15_000,
    retry: false,
  })
  const data = summary.data
  const items = useSummaryCardsConfig({
    remainDisplay: data ? formatAmount(data.quota) : '--',
    usedDisplay: data ? formatAmount(data.used_quota) : '--',
    netDisplay: data ? formatAmount(data.net_used_quota) : '--',
    refundDisplay: data ? formatAmount(data.refunded_quota) : '--',
    requestCountDisplay: data ? formatNumber(data.request_count) : '--',
    currencyLabel: getCurrencyLabel(),
    currencyEnabled: isCurrencyDisplayEnabled(),
  })

  return (
    <section
      aria-label={t('Account summary')}
      className='flex min-w-0 flex-col gap-3'
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <h3 className='text-sm font-medium'>{t('Account summary')}</h3>
        <div className='flex items-center gap-1'>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant='ghost'
                size='icon-sm'
                aria-label={t('Refresh account summary')}
                disabled={summary.isFetching}
                onClick={() => void summary.refetch()}
              >
                <RefreshCw
                  className={cn(summary.isFetching && 'animate-spin')}
                />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t('Refresh account summary')}</TooltipContent>
          </Tooltip>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => setHistoryOpen(true)}
          >
            <History data-icon='inline-start' />
            {t('Refunds and returns')}
          </Button>
          <Button variant='outline' size='sm' asChild>
            <Link to='/wallet'>
              <CreditCard data-icon='inline-start' />
              {t('Recharge')}
            </Link>
          </Button>
        </div>
      </div>
      <dl className='grid min-w-0 grid-cols-2 border-y lg:grid-cols-4'>
        {items.map((item, index) => {
          const Icon = item.icon
          return (
            <div
              key={item.key}
              className={cn(
                'flex min-h-32 min-w-0 flex-col gap-2 px-3 py-4 sm:px-5',
                index % 2 === 1 && 'border-l',
                index > 1 && 'border-t lg:border-t-0',
                index === 2 && 'lg:border-l'
              )}
            >
              <dt className='text-muted-foreground flex items-start gap-2 text-xs font-medium'>
                <Icon className='size-4 shrink-0' />
                <span>{item.title}</span>
              </dt>
              <dd className='min-h-8 text-2xl leading-8 font-semibold break-all tabular-nums'>
                {summary.isPending ? (
                  <Skeleton className='h-8 w-24 max-w-full' />
                ) : (
                  item.value
                )}
              </dd>
              <dd className='text-muted-foreground text-xs leading-5 break-words'>
                {item.description}
              </dd>
            </div>
          )
        })}
      </dl>
      {summary.isError && (
        <p role='alert' className='text-destructive text-sm'>
          {t('Failed to load account summary')}
          {data && ` ${t('Showing the last successful update')}`}
        </p>
      )}
      <p className='text-muted-foreground flex items-start gap-2 text-xs leading-5'>
        <Info className='mt-0.5 size-4 shrink-0' />
        <span>
          {t(
            'Net consumption equals settled consumption minus consumption returns. Returns are already included in your balance. Automatic task refunds are already deducted from settled consumption.'
          )}
        </span>
      </p>
      <PersonalRefundDialog open={historyOpen} onOpenChange={setHistoryOpen} />
    </section>
  )
}
