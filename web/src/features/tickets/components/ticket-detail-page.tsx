import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import { toast } from 'sonner'
import { ArrowLeft } from 'lucide-react'
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
import { Skeleton } from '@/components/ui/skeleton'
import { useAuthStore } from '@/stores/auth-store'
import { cn } from '@/lib/utils'
import {
  adminUpdateTicketStatus,
  closeTicket,
  fetchTicketDetail,
  postTicketReply,
} from '../api'
import {
  TICKET_PRIORITY_META,
  TICKET_STATUS_META,
  TICKET_TYPE_LABELS,
  canCloseTicket,
  canReplyTicket,
  formatTicketTimestamp,
} from '../lib/ticket-utils'
import {
  TICKET_STATUS,
  type Ticket,
  type TicketDetail,
  type TicketPriority,
  type TicketStatus,
} from '../types'
import { InvoiceDetailCard } from './invoice-detail-card'
import { RefundDetailCard } from './refund-detail-card'
import { TicketConversation } from './ticket-conversation'
import { TicketReplyBox } from './ticket-reply-box'
import { TicketUserProfileSheet } from './user-profile-sheet'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type Props = {
  ticketId: number
  admin: boolean
  /** Path to navigate back to (the list page). */
  backTo: string
}

export function TicketDetailPage({ ticketId, admin, backTo }: Props) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const currentUserId = useAuthStore((state) => state.auth.user?.id ?? 0)

  const [detail, setDetail] = useState<TicketDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [closeDialogOpen, setCloseDialogOpen] = useState(false)

  const loadDetail = useCallback(async () => {
    setLoading(true)
    try {
      const data = await fetchTicketDetail(ticketId, admin)
      setDetail(data)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to load ticket'))
      setDetail(null)
    } finally {
      setLoading(false)
    }
  }, [ticketId, admin, t])

  useEffect(() => {
    loadDetail()
  }, [loadDetail])

  const ticket: Ticket | undefined = detail?.ticket
  const messages = detail?.messages || []

  const handleReply = async (
    content: string,
    attachmentIds: number[]
  ): Promise<boolean> => {
    try {
      await postTicketReply(ticketId, content, admin, attachmentIds)
      toast.success(t('Reply sent'))
      await loadDetail()
      return true
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to send reply'))
      return false
    }
  }

  const handleClose = async () => {
    try {
      await closeTicket(ticketId, admin)
      toast.success(t('Ticket closed'))
      setCloseDialogOpen(false)
      await loadDetail()
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to close ticket')
      )
    }
  }

  const handleStatusChange = async (next: TicketStatus) => {
    try {
      await adminUpdateTicketStatus(ticketId, next)
      toast.success(t('Status updated'))
      await loadDetail()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to update status'))
    }
  }

  const statusMeta = useMemo(
    () => (ticket ? TICKET_STATUS_META[ticket.status as TicketStatus] : null),
    [ticket]
  )
  const priorityMeta = useMemo(
    () =>
      ticket
        ? TICKET_PRIORITY_META[
            ticket.priority as TicketPriority
          ] ?? TICKET_PRIORITY_META[2]
        : null,
    [ticket]
  )

  if (loading && !detail) {
    return (
      <div className='px-4 py-6 sm:px-8'>
        <div className='mx-auto w-full max-w-4xl space-y-4'>
          <Skeleton className='h-8 w-48' />
          <Skeleton className='h-32 w-full' />
          <Skeleton className='h-64 w-full' />
        </div>
      </div>
    )
  }

  if (!ticket) {
    return (
      <div className='px-4 py-6 sm:px-8'>
        <div className='mx-auto w-full max-w-4xl space-y-4'>
          <Button variant='ghost' onClick={() => navigate({ to: backTo })}>
            <ArrowLeft className='mr-1.5 h-3.5 w-3.5' />
            {t('Back to tickets')}
          </Button>
          <div className='bg-card text-muted-foreground rounded-2xl border p-12 text-center text-sm'>
            {t('Ticket not found')}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-4xl space-y-4'>
        <div className='bg-card rounded-2xl border p-5'>
          <div className='flex flex-col gap-3 md:flex-row md:items-start md:justify-between'>
            <div className='space-y-2'>
              <Button
                variant='ghost'
                size='sm'
                className='-ml-2'
                onClick={() => navigate({ to: backTo })}
              >
                <ArrowLeft className='mr-1.5 h-3.5 w-3.5' />
                {t('Back to tickets')}
              </Button>
              <div className='flex flex-wrap items-center gap-2'>
                {statusMeta && (
                  <Badge
                    variant='outline'
                    className={cn('font-normal', statusMeta.badgeClass)}
                  >
                    {t(statusMeta.labelKey)}
                  </Badge>
                )}
                <Badge variant='outline' className='font-normal'>
                  {t(TICKET_TYPE_LABELS[ticket.type] || ticket.type)}
                </Badge>
                {priorityMeta && (
                  <Badge
                    variant='outline'
                    className={cn('font-normal', priorityMeta.className)}
                  >
                    {t(priorityMeta.labelKey)}
                  </Badge>
                )}
              </div>
              <h1 className='text-xl font-semibold tracking-tight'>
                {ticket.subject || '—'}
              </h1>
              <p className='text-muted-foreground text-sm'>
                {admin
                  ? t('Manage and reply to the ticket on behalf of support')
                  : t(
                      'Continue the conversation here; admin replies stay in the same thread'
                    )}
              </p>
            </div>
            <div className='flex flex-wrap items-center gap-2'>
              {admin && (
                <TicketUserProfileSheet
                  ticketId={ticket.id}
                  username={ticket.username}
                  userId={ticket.user_id}
                />
              )}
              {admin && (
                <Select
                  value={String(ticket.status)}
                  onValueChange={(v) =>
                    handleStatusChange(Number(v) as TicketStatus)
                  }
                >
                  <SelectTrigger className='w-[140px]'>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {(
                      [
                        TICKET_STATUS.PENDING,
                        TICKET_STATUS.IN_PROGRESS,
                        TICKET_STATUS.RESOLVED,
                        TICKET_STATUS.CLOSED,
                      ] as const
                    ).map((s) => (
                      <SelectItem key={s} value={String(s)}>
                        {t(TICKET_STATUS_META[s].labelKey)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
              {canCloseTicket(ticket.status) && (
                <Button
                  variant='outline'
                  className='text-destructive hover:text-destructive'
                  onClick={() => setCloseDialogOpen(true)}
                >
                  {t('Close ticket')}
                </Button>
              )}
            </div>
          </div>

          <dl className='text-muted-foreground mt-4 grid gap-x-6 gap-y-2 text-xs sm:grid-cols-2 md:grid-cols-4'>
            <div>
              <dt className='font-medium'>ID</dt>
              <dd className='mt-0.5 font-mono'>#{ticket.id}</dd>
            </div>
            <div>
              <dt className='font-medium'>{t('Created')}</dt>
              <dd className='mt-0.5'>
                {formatTicketTimestamp(ticket.created_time)}
              </dd>
            </div>
            <div>
              <dt className='font-medium'>{t('Updated')}</dt>
              <dd className='mt-0.5'>
                {formatTicketTimestamp(ticket.updated_time)}
              </dd>
            </div>
            {admin && ticket.username && (
              <div>
                <dt className='font-medium'>{t('User')}</dt>
                <dd className='mt-0.5 font-mono'>{ticket.username}</dd>
              </div>
            )}
          </dl>

        </div>

        {detail?.invoice && (
          <InvoiceDetailCard
            ticket={ticket}
            invoice={detail.invoice}
            orders={detail.invoice_orders ?? []}
            admin={admin}
            onChanged={loadDetail}
          />
        )}

        {detail?.refund && (
          <RefundDetailCard
            ticket={ticket}
            refund={detail.refund}
            admin={admin}
            onChanged={loadDetail}
          />
        )}

        <TicketConversation
          messages={messages}
          currentUserId={admin ? undefined : currentUserId}
          loading={loading}
        />

        <TicketReplyBox
          disabled={!canReplyTicket(ticket.status)}
          onSubmit={handleReply}
        />
      </div>

      <AlertDialog open={closeDialogOpen} onOpenChange={setCloseDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Close this ticket?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(
                'You can still review the conversation history after closing.'
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
              onClick={handleClose}
            >
              {t('Close ticket')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
