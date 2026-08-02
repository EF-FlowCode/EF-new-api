// 仅组件——常量与工具函数搬到了 shared-utils.ts 满足 react-refresh。
// 为兼容历史 import 路径，本文件 re-export 工具：但这样还是会触发 lint。
// 所以新代码请直接 import { ... } from './shared-utils'。
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'

export function StatusBadge({
  status,
}: {
  status: 'normal' | 'observe' | 'blocked' | string
}) {
  const { t } = useTranslation()
  switch (status) {
    case 'blocked':
      return (
        <Badge
          variant='outline'
          className='border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
        >
          {t('Blocked')}
        </Badge>
      )
    case 'observe':
      return (
        <Badge
          variant='outline'
          className='border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
        >
          {t('Observing')}
        </Badge>
      )
    default:
      return (
        <Badge
          variant='outline'
          className='border-muted-foreground/40 bg-muted/40 text-muted-foreground'
        >
          {t('Normal')}
        </Badge>
      )
  }
}

export function DecisionBadge({
  decision,
}: {
  decision: 'allow' | 'observe' | 'block' | string
}) {
  const { t } = useTranslation()
  switch (decision) {
    case 'block':
      return (
        <Badge
          variant='outline'
          className='border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
        >
          {t('Block')}
        </Badge>
      )
    case 'observe':
      return (
        <Badge
          variant='outline'
          className='border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
        >
          {t('Observe')}
        </Badge>
      )
    default:
      return (
        <Badge
          variant='outline'
          className='border-muted-foreground/40 bg-muted/40 text-muted-foreground'
        >
          {t('Allow')}
        </Badge>
      )
  }
}

export function OverviewCard({
  title,
  value,
  hint,
  className,
}: {
  title: string
  value: React.ReactNode
  hint?: string
  className?: string
}) {
  return (
    <div className={cn('bg-card rounded-xl border p-4', className)}>
      <div className='text-muted-foreground text-xs'>{title}</div>
      <div className='mt-1 text-2xl font-semibold tracking-tight'>{value}</div>
      {hint && <div className='text-muted-foreground mt-1 text-xs'>{hint}</div>}
    </div>
  )
}
