import {
  Hash,
  Coins,
  Layers,
  Gauge,
  Zap,
  Wallet,
  TrendingUp,
  Activity,
  RotateCcw,
  type LucideIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { safeDivide } from '@/features/dashboard/lib'

interface StatCardConfig {
  key: string
  title: string
  description: string
  icon: LucideIcon
  getValue: (stat: Record<string, number>, days?: number) => number
}

export function useModelStatCardsConfig(): StatCardConfig[] {
  const { t } = useTranslation()

  return [
    {
      key: 'count',
      title: t('Total Count'),
      description: t('Statistical count'),
      icon: Hash,
      getValue: (stat) => stat?.rpm ?? 0,
    },
    {
      key: 'quota',
      title: t('Total Quota'),
      description: t('Statistical quota'),
      icon: Coins,
      getValue: (stat) => stat?.quota ?? 0,
    },
    {
      key: 'tokens',
      title: t('Total Tokens'),
      description: t('Statistical tokens'),
      icon: Layers,
      getValue: (stat) => stat?.tpm ?? 0,
    },
    {
      key: 'avgRpm',
      title: t('Average RPM'),
      description: t('Requests per minute'),
      icon: Gauge,
      getValue: (stat, timeRangeMinutes = 1) =>
        safeDivide(stat?.rpm ?? 0, timeRangeMinutes),
    },
    {
      key: 'avgTpm',
      title: t('Average TPM'),
      description: t('Tokens per minute'),
      icon: Zap,
      getValue: (stat, timeRangeMinutes = 1) =>
        safeDivide(stat?.tpm ?? 0, timeRangeMinutes),
    },
  ]
}

export function useSummaryCardsConfig(totals: {
  remainDisplay: string
  usedDisplay: string
  netDisplay: string
  refundDisplay: string
  requestCountDisplay: string
  currencyLabel: string
  currencyEnabled: boolean
}) {
  const { t } = useTranslation()

  return [
    {
      key: 'balance',
      title: t('Current Balance'),
      value: totals.remainDisplay,
      description: totals.currencyEnabled
        ? `${t('Remaining quota')} (${totals.currencyLabel})`
        : t('Remaining quota units'),
      icon: Wallet,
    },
    {
      key: 'usage',
      title: t('Net consumption'),
      value: totals.netDisplay,
      description: t('Settled consumption: {{amount}}', {
        amount: totals.usedDisplay,
      }),
      icon: TrendingUp,
    },
    {
      key: 'refunds',
      title: t('Consumption returned'),
      value: totals.refundDisplay,
      description: t('Credited to your balance'),
      icon: RotateCcw,
    },
    {
      key: 'requests',
      title: t('Request Count'),
      value: totals.requestCountDisplay,
      description: t('Total requests made'),
      icon: Activity,
    },
  ]
}
