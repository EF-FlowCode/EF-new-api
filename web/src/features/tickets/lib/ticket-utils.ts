import { TICKET_STATUS, type TicketStatus, type TicketType } from '../types'

export type TicketStatusMeta = {
  labelKey: string
  badgeClass: string
}

export const TICKET_STATUS_META: Record<TicketStatus, TicketStatusMeta> = {
  [TICKET_STATUS.PENDING]: {
    labelKey: 'Pending',
    badgeClass:
      'border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-400',
  },
  [TICKET_STATUS.IN_PROGRESS]: {
    labelKey: 'In progress',
    badgeClass:
      'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  },
  [TICKET_STATUS.RESOLVED]: {
    labelKey: 'Resolved',
    badgeClass:
      'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  },
  [TICKET_STATUS.CLOSED]: {
    labelKey: 'Closed',
    badgeClass:
      'border-muted-foreground/40 bg-muted/40 text-muted-foreground',
  },
}

export const TICKET_TYPE_LABELS: Record<TicketType, string> = {
  general: 'General',
  refund: 'Refund',
  invoice: 'Invoice',
}

export const TICKET_PRIORITY_META = {
  1: { labelKey: 'High priority', className: 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400' },
  2: { labelKey: 'Normal priority', className: 'border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-400' },
  3: { labelKey: 'Low priority', className: 'border-muted-foreground/40 bg-muted/40 text-muted-foreground' },
} as const

export function canReplyTicket(status: TicketStatus | undefined): boolean {
  return status !== TICKET_STATUS.CLOSED
}

export function canCloseTicket(status: TicketStatus | undefined): boolean {
  return status !== TICKET_STATUS.CLOSED
}

export function formatTicketTimestamp(unixSec: number | undefined): string {
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

/**
 * Map TicketMessage.role to a friendly badge.
 *   0  - User
 *   5  - Customer support
 *   10 - Admin
 *   100+ - Super admin
 */
export function resolveRoleBadge(role: number | undefined): {
  labelKey: string
  className: string
} {
  const r = Number(role || 0)
  if (r >= 100) {
    return {
      labelKey: 'Super admin',
      className:
        'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400',
    }
  }
  if (r >= 10) {
    return {
      labelKey: 'Admin',
      className:
        'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
    }
  }
  if (r >= 5) {
    return {
      labelKey: 'Support',
      className:
        'border-cyan-500/40 bg-cyan-500/10 text-cyan-600 dark:text-cyan-400',
    }
  }
  return {
    labelKey: 'User',
    className:
      'border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-400',
  }
}
