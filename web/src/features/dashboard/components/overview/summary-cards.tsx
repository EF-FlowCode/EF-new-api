import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { CreditCard, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { getCurrencyLabel, isCurrencyDisplayEnabled } from '@/lib/currency'
import { getSelf } from '@/lib/api'
import { formatNumber, formatQuota } from '@/lib/format'
import { useStatus } from '@/hooks/use-status'
import { Button } from '@/components/ui/button'
import { StaggerContainer, StaggerItem } from '@/components/page-transition'
import { useSummaryCardsConfig } from '@/features/dashboard/hooks/use-dashboard-config'
import { StatCard } from '../ui/stat-card'

export function SummaryCards() {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const setAuthUser = useAuthStore((state) => state.auth.setUser)
  const { status, loading } = useStatus()
  // eflow: 余额卡上的手动刷新按钮——loading 防抖、用 toast 给成功/失败回执。
  // 与下方 useEffect 共用同一个 fetcher，避免逻辑分叉。
  const [refreshing, setRefreshing] = useState(false)

  // mount 时静默拉一次，与「钱包页」保持一致。
  // 失败不打扰用户（仍显示缓存值），手动按钮会给明确反馈。
  const refreshSelf = useCallback(
    async (notify: boolean): Promise<boolean> => {
      try {
        const res = await getSelf()
        if (res.success && res.data) {
          setAuthUser(res.data as never)
          if (notify) toast.success(t('Balance refreshed'))
          return true
        }
        if (notify) {
          toast.error(res.message || t('Failed to refresh balance'))
        }
        return false
      } catch (e) {
        if (notify) {
          toast.error((e as Error).message || t('Failed to refresh balance'))
        }
        return false
      }
    },
    [setAuthUser, t]
  )

  // Fetch fresh user data each time the overview mounts so the balance
  // here stays in sync with the Wallet page (which also updates auth-store).
  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const res = await getSelf()
        if (!cancelled && res.success && res.data) {
          setAuthUser(res.data as never)
        }
      } catch {
        /* swallow — keep showing the cached value */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [setAuthUser])

  const onRefreshBalance = useCallback(async () => {
    if (refreshing) return
    setRefreshing(true)
    try {
      await refreshSelf(true)
    } finally {
      setRefreshing(false)
    }
  }, [refreshing, refreshSelf])

  const summaryValues = useMemo(() => {
    const remainQuota = Number(user?.quota ?? 0)
    const usedQuota = Number(user?.used_quota ?? 0)
    const requestCount = Number(user?.request_count ?? 0)

    return {
      remainDisplay: formatQuota(remainQuota),
      usedDisplay: formatQuota(usedQuota),
      requestCountDisplay: formatNumber(requestCount),
    }
  }, [user])

  const currencyEnabledFromStore = isCurrencyDisplayEnabled()
  const statusCurrencyFlag =
    typeof status?.display_in_currency === 'boolean'
      ? Boolean(status.display_in_currency)
      : undefined
  const currencyEnabled =
    statusCurrencyFlag !== undefined
      ? statusCurrencyFlag
      : currencyEnabledFromStore
  const currencyLabel = currencyEnabled ? getCurrencyLabel() : 'Tokens'

  const items = useSummaryCardsConfig({
    ...summaryValues,
    currencyEnabled,
    currencyLabel,
  }).map((config, index) => ({
    title: config.title,
    value: config.value,
    desc: config.description,
    icon: config.icon,
    isBalance: index === 0,
  }))

  return (
    <div className='overflow-hidden rounded-lg border'>
      <StaggerContainer className='divide-border/60 grid grid-cols-3 divide-x'>
        {items.map((it) => (
          <StaggerItem key={it.title} className='px-3 py-3 sm:px-5 sm:py-4'>
            <StatCard
              title={it.title}
              value={it.value}
              description={it.desc}
              icon={it.icon}
              loading={loading}
              action={
                it.isBalance ? (
                  // eflow: 刷新按钮 + 充值按钮并列；手机窄屏只保留刷新（充值在大屏 sm+ 才出现）。
                  <div className='flex items-center gap-1'>
                    <Button
                      variant='ghost'
                      size='sm'
                      className='h-6 gap-1 px-2 text-xs'
                      onClick={() => void onRefreshBalance()}
                      disabled={refreshing}
                      title={t('Refresh balance')}
                    >
                      <RefreshCw
                        className={
                          refreshing ? 'size-3 animate-spin' : 'size-3'
                        }
                      />
                      <span className='hidden sm:inline'>{t('Refresh')}</span>
                    </Button>
                    <Button
                      variant='outline'
                      size='sm'
                      className='hidden h-6 gap-1 px-2 text-xs sm:inline-flex'
                      asChild
                    >
                      <Link to='/wallet'>
                        <CreditCard className='size-3' />
                        {t('Recharge')}
                      </Link>
                    </Button>
                  </div>
                ) : undefined
              }
            />
          </StaggerItem>
        ))}
      </StaggerContainer>
    </div>
  )
}
