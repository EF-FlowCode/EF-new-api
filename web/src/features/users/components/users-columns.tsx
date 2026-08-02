import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { formatQuota, formatTimestamp } from '@/lib/format'
import { cn } from '@/lib/utils'
import { useIsStaff } from '@/hooks/use-admin'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Progress } from '@/components/ui/progress'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { DataTableColumnHeader } from '@/components/data-table'
import { GroupBadge } from '@/components/group-badge'
import { LongText } from '@/components/long-text'
import { StatusBadge, dotColorMap } from '@/components/status-badge'
import { USER_STATUSES, USER_ROLES, isUserDeleted } from '../constants'
import { type User } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { useUsers } from './users-provider'

function getQuotaProgressColor(percentage: number): string {
  if (percentage <= 10) return '[&_[data-slot=progress-indicator]]:bg-rose-500'
  if (percentage <= 30) return '[&_[data-slot=progress-indicator]]:bg-amber-500'
  return '[&_[data-slot=progress-indicator]]:bg-emerald-500'
}

export function useUsersColumns(): ColumnDef<User>[] {
  const { t } = useTranslation()
  const { setProfileUserId } = useUsers()
  // eflow: 客服 (role>=5) 与管理员都能点用户名弹画像。普通用户看到纯文本。
  // 后端 GET /api/user/:id/profile-summary 走 TicketStaffAuth，三道防线一致。
  const isStaff = useIsStaff()
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
      accessorKey: 'id',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title='ID' />
      ),
      cell: ({ row }) => {
        return <div className='w-[60px]'>{row.getValue('id')}</div>
      },
      meta: { label: t('ID'), mobileHidden: true },
    },
    {
      accessorKey: 'username',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Username')} />
      ),
      cell: ({ row }) => {
        const username = row.getValue('username') as string
        const displayName = row.original.display_name
        const remark = row.original.remark
        const id = row.original.id

        return (
          <div className='flex min-w-[160px] flex-col gap-1'>
            <div className='flex items-center gap-2'>
              {/* eflow: staff (admin + 客服) 可点 → 弹用户画像 sheet。普通用户纯文本。 */}
              {isStaff ? (
                <button
                  type='button'
                  className='hover:text-primary text-left transition'
                  onClick={(e) => {
                    e.stopPropagation()
                    if (id != null) setProfileUserId(id)
                  }}
                >
                  <LongText className='max-w-[140px] font-medium hover:underline'>
                    {username}
                  </LongText>
                </button>
              ) : (
                <LongText className='max-w-[140px] font-medium'>
                  {username}
                </LongText>
              )}
              {remark && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <StatusBadge variant='success' copyable={false}>
                      <LongText className='max-w-[80px]'>{remark}</LongText>
                    </StatusBadge>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p className='text-xs'>{remark}</p>
                  </TooltipContent>
                </Tooltip>
              )}
            </div>
            {displayName && displayName !== username && (
              <LongText className='text-muted-foreground max-w-[180px] text-xs'>
                {displayName}
              </LongText>
            )}
          </div>
        )
      },
      enableHiding: false,
      meta: { label: t('Username'), mobileTitle: true },
    },
    {
      accessorKey: 'status',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Status')} />
      ),
      cell: ({ row }) => {
        const user = row.original
        const requestCount = user.request_count

        const statusConfig = isUserDeleted(user)
          ? USER_STATUSES.DELETED
          : USER_STATUSES[user.status as keyof typeof USER_STATUSES]

        if (!statusConfig) {
          return null
        }

        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className='cursor-help'>
                <StatusBadge
                  label={t(statusConfig.labelKey)}
                  variant={statusConfig.variant}
                  showDot={statusConfig.showDot}
                  copyable={false}
                />
              </div>
            </TooltipTrigger>
            <TooltipContent>
              <p className='text-xs'>
                {t('Requests:')} {requestCount.toLocaleString()}
              </p>
            </TooltipContent>
          </Tooltip>
        )
      },
      filterFn: (_row, _id, _value) => {
        // eflow: status 改后端按 IN (?) 过滤；本地 filterFn 退化为永真，
        // 否则跨页时会把后端返回的数据再做一次 client-side 过滤，导致看上去丢行。
        return true
      },
      enableSorting: false,
      meta: { label: t('Status'), mobileBadge: true },
    },
    {
      id: 'quota',
      accessorKey: 'quota',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Quota')} />
      ),
      cell: ({ row }) => {
        const user = row.original
        const used = user.used_quota
        const remaining = user.quota
        const total = used + remaining
        const percentage = total > 0 ? (remaining / total) * 100 : 0

        if (total === 0) {
          return (
            <StatusBadge
              label={t('No Quota')}
              variant='neutral'
              copyable={false}
            />
          )
        }

        return (
          <Tooltip>
            <TooltipTrigger asChild>
              <div className='w-[150px] cursor-help space-y-1'>
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
                  {t('Remaining:')} {formatQuota(remaining)}
                </div>
                <div>
                  {t('Total:')} {formatQuota(total)}
                </div>
                <div>
                  {t('Percentage:')} {percentage.toFixed(1)}%
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
        <DataTableColumnHeader
          column={column}
          title={t('Customer tier / additional tiers')}
        />
      ),
      cell: ({ row }) => {
        const group = row.getValue('group') as string
        const additionalTiers = row.original.additional_customer_tiers || []
        return (
          <div className='min-w-[180px] space-y-1.5'>
            <GroupBadge group={group} />
            {additionalTiers.length === 0 ? (
              <span className='text-muted-foreground block text-[11px]'>
                {t('Primary tier only')}
              </span>
            ) : (
              <div className='flex max-w-[260px] flex-wrap gap-1'>
                {additionalTiers.slice(0, 2).map((tier) => (
                  <Badge
                    key={tier}
                    variant='outline'
                    className='max-w-[110px] px-1.5 py-0 text-[10px]'
                    title={tier}
                  >
                    <span className='truncate'>{tier}</span>
                  </Badge>
                ))}
                {additionalTiers.length > 2 && (
                  <Badge
                    variant='secondary'
                    className='px-1.5 py-0 text-[10px]'
                    title={additionalTiers.slice(2).join(', ')}
                  >
                    +{additionalTiers.length - 2}
                  </Badge>
                )}
              </div>
            )}
          </div>
        )
      },
      filterFn: (_row, _id, _value) => {
        // eflow: group 改后端按 = ? 精确过滤；本地 filterFn 退化为永真。
        // 原来用 includes 子串匹配是给"模糊搜索"用的，但 group 现在走单选 facet 精确匹配，
        // 加上后端已经过滤，本地不能再插一脚。
        return true
      },
      enableSorting: false,
      meta: { label: t('Customer tier / additional tiers') },
    },
    {
      accessorKey: 'role',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Role')} />
      ),
      cell: ({ row }) => {
        const roleValue = row.getValue('role') as number
        const roleConfig = USER_ROLES[roleValue as keyof typeof USER_ROLES]

        if (!roleConfig) {
          return null
        }

        return (
          <div className='flex items-center gap-x-2'>
            {roleConfig.icon && (
              <roleConfig.icon size={16} className='text-muted-foreground' />
            )}
            <span className='text-sm'>{t(roleConfig.labelKey)}</span>
          </div>
        )
      },
      filterFn: (_row, _id, _value) => {
        // eflow: role 改后端按 IN (?) 过滤；本地 filterFn 退化为永真,
        // 避免 value 短暂为空数组(取消多选瞬间)时 react-table 本地"全否"导致空表。
        return true
      },
      enableSorting: false,
      meta: { label: t('Role') },
    },
    {
      id: 'invite_info',
      accessorFn: (user) => user.inviter_id,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Invite Info')} />
      ),
      cell: ({ row }) => {
        const user = row.original
        const affCount = user.aff_count || 0
        const affHistoryQuota = user.aff_history_quota || 0
        const inviterId = user.inviter_id || 0

        return (
          <div className='flex items-center gap-1.5 text-xs font-medium'>
            <span
              className={cn(
                'size-1.5 shrink-0 rounded-full',
                dotColorMap.neutral
              )}
              aria-hidden='true'
            />
            <Tooltip>
              <TooltipTrigger asChild>
                <span className='text-muted-foreground cursor-help'>
                  {t('Invited')}: {affCount}
                </span>
              </TooltipTrigger>
              <TooltipContent>
                <p className='text-xs'>{t('Number of users invited')}</p>
              </TooltipContent>
            </Tooltip>
            <span className='text-muted-foreground/30'>·</span>
            <Tooltip>
              <TooltipTrigger asChild>
                <span className='text-muted-foreground cursor-help'>
                  {t('Revenue')}: {formatQuota(affHistoryQuota)}
                </span>
              </TooltipTrigger>
              <TooltipContent>
                <p className='text-xs'>{t('Total invitation revenue')}</p>
              </TooltipContent>
            </Tooltip>
            {inviterId > 0 && (
              <>
                <span className='text-muted-foreground/30'>·</span>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span className='text-muted-foreground cursor-help'>
                      {t('Inviter')}: {inviterId}
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>
                    <p className='text-xs'>
                      {t('Invited by user ID')} {inviterId}
                    </p>
                  </TooltipContent>
                </Tooltip>
              </>
            )}
            {inviterId === 0 && (
              <>
                <span className='text-muted-foreground/30'>·</span>
                <span className='text-muted-foreground'>{t('No Inviter')}</span>
              </>
            )}
          </div>
        )
      },
      enableSorting: false,
      meta: { label: t('Invite Info'), mobileHidden: true },
    },
    {
      accessorKey: 'created_at',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Created At')} />
      ),
      cell: ({ row }) => {
        const ts = row.getValue('created_at') as number | undefined
        return (
          <span className='text-muted-foreground text-sm'>
            {ts ? formatTimestamp(ts) : '-'}
          </span>
        )
      },
      meta: { label: t('Created At'), mobileHidden: true },
    },
    {
      accessorKey: 'last_login_at',
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Last Login')} />
      ),
      cell: ({ row }) => {
        const ts = row.getValue('last_login_at') as number | undefined
        return (
          <span className='text-muted-foreground text-sm'>
            {ts ? formatTimestamp(ts) : '-'}
          </span>
        )
      },
      meta: { label: t('Last Login'), mobileHidden: true },
    },
    {
      id: 'actions',
      cell: ({ row }) => <DataTableRowActions row={row} />,
      meta: { label: t('Actions') },
    },
  ]
}
