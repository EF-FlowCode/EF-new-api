import { useQuery } from '@tanstack/react-query'
import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { getUserGroups } from '@/lib/api'
import { formatQuota, formatTimestampToDate } from '@/lib/format'
import { cn } from '@/lib/utils'
import { Checkbox } from '@/components/ui/checkbox'
import { Progress } from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { DataTableColumnHeader } from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { StatusBadge } from '@/components/status-badge'
import { API_KEY_STATUSES } from '../constants'
import { type ApiKey } from '../types'
import {
  ApiKeyCell,
  IpRestrictionsCell,
  ModelLimitsCell,
  UnlimitedQuotaBadge,
} from './api-keys-cells'
import { DataTableRowActions } from './data-table-row-actions'

function getQuotaProgressColor(percentage: number): string {
  if (percentage <= 10) return '[&_[data-slot=progress-indicator]]:bg-rose-500'
  if (percentage <= 30) return '[&_[data-slot=progress-indicator]]:bg-amber-500'
  return '[&_[data-slot=progress-indicator]]:bg-emerald-500'
}

function useAuthorizedGroupRatios() {
  const query = useQuery({
    queryKey: ['user-self-groups'],
    queryFn: getUserGroups,
    staleTime: 30 * 1000,
    refetchInterval: 30 * 1000,
    refetchOnWindowFocus: true,
    select: (res) => {
      if (!res.success || !res.data) {
        return {
          ratios: {},
          authorizedGroups: [] as string[],
          unavailableGroups: [] as string[],
        }
      }
      const ratios: Record<string, number> = {}
      const unavailableGroups: string[] = []
      for (const [group, info] of Object.entries(res.data)) {
        if (typeof info.ratio === 'number') {
          ratios[group] = info.ratio
        }
        if (info.available === false) {
          unavailableGroups.push(group)
        }
      }
      return {
        ratios,
        authorizedGroups: Object.keys(res.data),
        unavailableGroups,
      }
    },
  })

  return {
    ratios: query.data?.ratios ?? {},
    authorizedGroups: query.data?.authorizedGroups ?? [],
    unavailableGroups: query.data?.unavailableGroups ?? [],
    loaded: query.isSuccess,
  }
}

export function useApiKeysColumns(): ColumnDef<ApiKey>[] {
  const { t } = useTranslation()
  const {
    ratios: groupRatios,
    authorizedGroups,
    unavailableGroups,
    loaded: groupAuthorizationLoaded,
  } = useAuthorizedGroupRatios()
  const primaryGroup = useAuthStore((state) => state.auth.user?.group)
  const authorizedGroupSet = new Set(authorizedGroups)
  const unavailableGroupSet = new Set(unavailableGroups)
  return [
    {
      id: 'select',
      header: ({ table }) => (
        <Checkbox
          checked={
            table.getIsAllPageRowsSelected() ||
            (table.getIsSomePageRowsSelected() && 'indeterminate')
          }
          onCheckedChange={(value) => table.toggleAllPageRowsSelected(!!value)}
          aria-label='Select all'
          className='translate-y-[2px]'
        />
      ),
      cell: ({ row }) => (
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(value) => row.toggleSelected(!!value)}
          aria-label='Select row'
          className='translate-y-[2px]'
        />
      ),
      enableSorting: false,
      enableHiding: false,
      meta: { label: t('Select') },
    },
    {
      accessorKey: 'name',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Name')} />
      ),
      cell: ({ row }) => (
        <div className='max-w-[200px] truncate font-medium'>
          {row.getValue('name')}
        </div>
      ),
      meta: { label: t('Name'), mobileTitle: true },
    },
    {
      accessorKey: 'status',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Status')} />
      ),
      cell: ({ row }) => {
        const statusConfig = API_KEY_STATUSES[row.getValue('status') as number]
        if (!statusConfig) return null
        return (
          <StatusBadge
            label={t(statusConfig.label)}
            variant={statusConfig.variant}
            showDot={statusConfig.showDot}
            copyable={false}
          />
        )
      },
      filterFn: (row, id, value) => value.includes(String(row.getValue(id))),
      enableSorting: false,
      meta: { label: t('Status'), mobileBadge: true },
    },
    {
      id: 'key',
      accessorKey: 'key',
      header: t('API Key'),
      cell: ({ row }) => <ApiKeyCell apiKey={row.original} />,
      enableSorting: false,
      meta: { label: t('API Key') },
    },
    {
      id: 'remain_quota',
      accessorKey: 'remain_quota',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Quota')} />
      ),
      cell: ({ row }) => {
        const apiKey = row.original
        if (apiKey.unlimited_quota) {
          return <UnlimitedQuotaBadge used={apiKey.used_quota} />
        }

        const used = apiKey.used_quota
        const remaining = apiKey.remain_quota
        const total = used + remaining
        const percentage = total > 0 ? (remaining / total) * 100 : 0

        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className='w-[150px] space-y-1'>
                <div className='flex justify-between text-xs'>
                  <span className='font-medium tabular-nums'>
                    {formatQuota(remaining)}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {formatQuota(total)}
                  </span>
                </div>
                <Progress
                  value={percentage}
                  className={cn('h-1.5', getQuotaProgressColor(percentage))}
                />
              </div>
            </TooltipTrigger>
            <TooltipContent>
              <div className='space-y-1 text-xs'>
                <div>
                  {t('Used:')} {formatQuota(used)}
                </div>
                <div>
                  {t('Remaining:')} {formatQuota(remaining)} (
                  {percentage.toFixed(1)}%)
                </div>
                <div>
                  {t('Total:')} {formatQuota(total)}
                </div>
              </div>
            </TooltipContent>
          </Tooltip>
        )
      },
      meta: { label: t('Quota') },
    },
    {
      accessorKey: 'group',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Group')} />
      ),
      cell: ({ row }) => {
        const apiKey = row.original
        const group = row.getValue('group') as string
        const ratio = group && group !== 'auto' ? groupRatios[group] : undefined
        // Same rule as the relay auth check: a token without a group falls
        // back to the primary customer tier, which must itself be authorized.
        const groupRemoved =
          groupAuthorizationLoaded &&
          (group
            ? !authorizedGroupSet.has(group)
            : Boolean(primaryGroup) && !authorizedGroupSet.has(primaryGroup!))
        const groupUnavailable =
          groupAuthorizationLoaded &&
          Boolean(group) &&
          unavailableGroupSet.has(group)
        const removedHint = groupRemoved && (
          <p className='text-destructive max-w-40 text-[11px] leading-tight'>
            {group
              ? t(
                  'This group was removed by the administrator. The token is unavailable.'
                )
              : t(
                  'Your primary tier is not authorized. Select an available group for this token.'
                )}
          </p>
        )

        if (group === 'auto') {
          return (
            <div className='space-y-1'>
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className='inline-flex items-center gap-1.5 text-xs'>
                    <GroupBadge group='auto' />
                    {apiKey.cross_group_retry && (
                      <>
                        <span className='text-muted-foreground/30'>·</span>
                        <span className='text-muted-foreground/60'>
                          {t('Cross-group')}
                        </span>
                      </>
                    )}
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  <span className='text-xs'>
                    {t(
                      'Automatically selects the best available group with circuit breaker mechanism'
                    )}
                  </span>
                </TooltipContent>
              </Tooltip>
              {removedHint}
            </div>
          )
        }
        return (
          <div className='space-y-1'>
            <GroupBadge group={group} ratio={ratio} />
            {removedHint}
            {groupUnavailable && (
              <p className='text-destructive max-w-40 text-[11px] leading-tight'>
                {t('Unavailable in this region')}
              </p>
            )}
          </div>
        )
      },
      enableSorting: false,
      meta: { label: t('Group') },
    },
    {
      id: 'model_limits',
      accessorKey: 'model_limits',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Models')} />
      ),
      cell: ({ row }) => <ModelLimitsCell apiKey={row.original} />,
      enableSorting: false,
      meta: { label: t('Models'), mobileHidden: true },
    },
    {
      id: 'allow_ips',
      accessorKey: 'allow_ips',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('IP Restriction')} />
      ),
      cell: ({ row }) => <IpRestrictionsCell apiKey={row.original} />,
      enableSorting: false,
      meta: { label: t('IP Restriction'), mobileHidden: true },
    },
    {
      accessorKey: 'created_time',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Created')} />
      ),
      cell: ({ row }) => (
        <span className='text-muted-foreground font-mono text-xs tabular-nums'>
          {formatTimestampToDate(row.getValue('created_time'))}
        </span>
      ),
      meta: { label: t('Created'), mobileHidden: true },
    },
    {
      accessorKey: 'accessed_time',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Last Used')} />
      ),
      cell: ({ row }) => {
        const accessedTime = row.getValue('accessed_time') as number
        if (!accessedTime) {
          return <span className='text-muted-foreground text-xs'>-</span>
        }
        return (
          <span className='text-muted-foreground font-mono text-xs tabular-nums'>
            {formatTimestampToDate(accessedTime)}
          </span>
        )
      },
      enableSorting: false,
      meta: { label: t('Last Used'), mobileHidden: true },
    },
    {
      accessorKey: 'expired_time',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Expires')} />
      ),
      cell: ({ row }) => {
        const expiredTime = row.getValue('expired_time') as number
        if (expiredTime === -1) {
          return (
            <StatusBadge
              label={t('Never')}
              variant='neutral'
              copyable={false}
            />
          )
        }
        const isExpired = expiredTime * 1000 < Date.now()
        return (
          <span
            className={cn(
              'font-mono text-xs tabular-nums',
              isExpired ? 'text-destructive' : 'text-muted-foreground'
            )}
          >
            {formatTimestampToDate(expiredTime)}
          </span>
        )
      },
      enableSorting: false,
      meta: { label: t('Expires'), mobileHidden: true },
    },
    {
      id: 'actions',
      cell: ({ row }) => <DataTableRowActions row={row} />,
      meta: { label: t('Actions') },
      size: 88,
    },
  ]
}
