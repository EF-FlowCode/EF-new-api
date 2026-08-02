import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { TicketSortField } from '../api'
import {
  formatTicketTimestamp,
  TICKET_PRIORITY_META,
  TICKET_STATUS_META,
  TICKET_TYPE_LABELS,
} from '../lib/ticket-utils'
import type { Ticket, TicketPriority, TicketStatus } from '../types'

type Props = {
  tickets: Ticket[]
  loading: boolean
  showAssignee?: boolean
  onRowClick: (ticket: Ticket) => void
  sortField?: TicketSortField | null
  sortDir?: 'asc' | 'desc'
  onSort?: (field: TicketSortField) => void
  /** 开启后最左侧出现勾选列（仅管理/客服侧用于批量操作）。 */
  selectable?: boolean
  selectedIds?: Set<number>
  onToggleSelect?: (id: number) => void
  /** 表头全选：勾上当前页全部、取消则清空当前页。 */
  onToggleSelectAll?: (checked: boolean) => void
}

type SortHeadProps = {
  field: TicketSortField
  label: string
  className?: string
  sortField: TicketSortField | null
  sortDir: 'asc' | 'desc'
  onSort?: (field: TicketSortField) => void
}

function SortHead({
  field,
  label,
  className,
  sortField,
  sortDir,
  onSort,
}: SortHeadProps) {
  const active = sortField === field
  const Icon = !active ? ArrowUpDown : sortDir === 'asc' ? ArrowUp : ArrowDown
  return (
    <TableHead className={className}>
      <button
        type='button'
        onClick={() => onSort?.(field)}
        className={cn(
          'hover:text-foreground -mx-1 inline-flex items-center gap-1 rounded px-1',
          active ? 'text-foreground font-medium' : 'text-muted-foreground'
        )}
      >
        {label}
        <Icon className='h-3.5 w-3.5 shrink-0 opacity-70' />
      </button>
    </TableHead>
  )
}

export function TicketsTable({
  tickets,
  loading,
  showAssignee = false,
  onRowClick,
  sortField = null,
  sortDir = 'desc',
  onSort,
  selectable = false,
  selectedIds,
  onToggleSelect,
  onToggleSelectAll,
}: Props) {
  const { t } = useTranslation()
  const selectCol = selectable ? 1 : 0
  const colCount = (showAssignee ? 8 : 7) + selectCol
  const selectedSet = selectedIds ?? new Set<number>()
  const pageIds = tickets.map((tk) => tk.id)
  const allOnPageSelected =
    pageIds.length > 0 && pageIds.every((id) => selectedSet.has(id))
  const someOnPageSelected =
    !allOnPageSelected && pageIds.some((id) => selectedSet.has(id))

  const sortProps = { sortField, sortDir, onSort }

  return (
    <>
      {!selectable && (
        <div className='bg-card divide-y overflow-hidden rounded-xl border sm:hidden'>
          {loading && tickets.length === 0 ? (
            Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className='space-y-2 p-4'>
                <Skeleton className='h-5 w-2/3' />
                <Skeleton className='h-4 w-1/2' />
              </div>
            ))
          ) : tickets.length === 0 ? (
            <div className='text-muted-foreground p-12 text-center text-sm'>
              {t('No tickets')}
            </div>
          ) : (
            tickets.map((ticket) => {
              const statusMeta =
                TICKET_STATUS_META[ticket.status as TicketStatus] ?? null
              const priorityMeta =
                TICKET_PRIORITY_META[ticket.priority as TicketPriority] ??
                TICKET_PRIORITY_META[2]
              return (
                <button
                  key={ticket.id}
                  type='button'
                  onClick={() => onRowClick(ticket)}
                  className='hover:bg-accent/40 w-full space-y-2 p-4 text-left transition-colors'
                >
                  <div className='flex items-start justify-between gap-3'>
                    <span className='line-clamp-2 text-sm font-medium'>
                      {ticket.subject}
                    </span>
                    <span className='text-muted-foreground shrink-0 font-mono text-xs'>
                      #{ticket.id}
                    </span>
                  </div>
                  <div className='flex flex-wrap items-center gap-1.5'>
                    <Badge variant='outline' className='font-normal'>
                      {t(TICKET_TYPE_LABELS[ticket.type] || ticket.type)}
                    </Badge>
                    {statusMeta && (
                      <Badge
                        variant='outline'
                        className={cn('font-normal', statusMeta.badgeClass)}
                      >
                        {t(statusMeta.labelKey)}
                      </Badge>
                    )}
                    <Badge
                      variant='outline'
                      className={cn('font-normal', priorityMeta.className)}
                    >
                      {t(priorityMeta.labelKey)}
                    </Badge>
                    <span className='text-muted-foreground ml-auto text-xs'>
                      {formatTicketTimestamp(ticket.updated_time)}
                    </span>
                  </div>
                </button>
              )
            })
          )}
        </div>
      )}

      <div
        className={cn(
          'bg-card overflow-x-auto rounded-xl border',
          !selectable && 'hidden sm:block'
        )}
      >
        <Table>
          <TableHeader>
            <TableRow>
              {selectable && (
                <TableHead className='w-10'>
                  <Checkbox
                    aria-label={t('Select all on this page')}
                    checked={
                      allOnPageSelected
                        ? true
                        : someOnPageSelected
                          ? 'indeterminate'
                          : false
                    }
                    onCheckedChange={(v) => onToggleSelectAll?.(v === true)}
                  />
                </TableHead>
              )}
              <SortHead field='id' label='ID' className='w-16' {...sortProps} />
              <TableHead>{t('Subject')}</TableHead>
              <TableHead className='w-24'>{t('Type')}</TableHead>
              <SortHead
                field='status'
                label={t('Status')}
                className='w-28'
                {...sortProps}
              />
              <SortHead
                field='priority'
                label={t('Priority')}
                className='w-28'
                {...sortProps}
              />
              {showAssignee && (
                <TableHead className='w-32'>{t('User')}</TableHead>
              )}
              <SortHead
                field='created_time'
                label={t('Created')}
                className='w-40'
                {...sortProps}
              />
              <SortHead
                field='updated_time'
                label={t('Updated')}
                className='w-40'
                {...sortProps}
              />
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && tickets.length === 0 ? (
              Array.from({ length: 5 }).map((_, i) => (
                <TableRow key={i}>
                  {Array.from({ length: colCount }).map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton className='h-4 w-full' />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : tickets.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={colCount}
                  className='text-muted-foreground py-12 text-center text-sm'
                >
                  {t('No tickets')}
                </TableCell>
              </TableRow>
            ) : (
              tickets.map((ticket) => {
                const statusMeta =
                  TICKET_STATUS_META[ticket.status as TicketStatus] ?? null
                const priorityMeta =
                  TICKET_PRIORITY_META[ticket.priority as TicketPriority] ??
                  TICKET_PRIORITY_META[2]
                return (
                  <TableRow
                    key={ticket.id}
                    onClick={() => onRowClick(ticket)}
                    className='hover:bg-accent/40 cursor-pointer'
                  >
                    {selectable && (
                      <TableCell
                        onClick={(e) => e.stopPropagation()}
                        className='w-10'
                      >
                        <Checkbox
                          aria-label={t('Select ticket')}
                          checked={selectedSet.has(ticket.id)}
                          onCheckedChange={() => onToggleSelect?.(ticket.id)}
                        />
                      </TableCell>
                    )}
                    <TableCell className='font-mono'>{ticket.id}</TableCell>
                    <TableCell
                      className='max-w-[280px] truncate'
                      title={ticket.subject}
                    >
                      {ticket.subject}
                    </TableCell>
                    <TableCell>
                      <Badge variant='outline' className='font-normal'>
                        {t(TICKET_TYPE_LABELS[ticket.type] || ticket.type)}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      {statusMeta ? (
                        <Badge
                          variant='outline'
                          className={cn('font-normal', statusMeta.badgeClass)}
                        >
                          {t(statusMeta.labelKey)}
                        </Badge>
                      ) : (
                        <span className='text-muted-foreground text-xs'>—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant='outline'
                        className={cn('font-normal', priorityMeta.className)}
                      >
                        {t(priorityMeta.labelKey)}
                      </Badge>
                    </TableCell>
                    {showAssignee && (
                      <TableCell className='font-mono text-xs'>
                        {ticket.username || `#${ticket.user_id}`}
                      </TableCell>
                    )}
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTicketTimestamp(ticket.created_time)}
                    </TableCell>
                    <TableCell className='text-muted-foreground text-xs'>
                      {formatTicketTimestamp(ticket.updated_time)}
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>
    </>
  )
}
