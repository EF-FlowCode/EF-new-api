import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Copy } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Textarea } from '@/components/ui/textarea'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useIsAdmin } from '@/hooks/use-admin'
import {
  displayAmountToQuota,
  formatQuotaWithCurrency,
} from '@/lib/currency'
import { cn } from '@/lib/utils'
import { fetchUserQuota, postTicketReply, updateRefundStatus } from '../api'
import { formatTicketTimestamp } from '../lib/ticket-utils'
import {
  REFUND_STATUS,
  type RefundPayeeType,
  type RefundQuotaMode,
  type RefundStatus,
  type Ticket,
  type TicketRefund,
} from '../types'

const REFUND_STATUS_META: Record<
  RefundStatus,
  { labelKey: string; className: string }
> = {
  [REFUND_STATUS.PENDING]: {
    labelKey: 'Pending review',
    className:
      'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  },
  [REFUND_STATUS.REFUNDED]: {
    labelKey: 'Refunded',
    className:
      'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  },
  [REFUND_STATUS.REJECTED]: {
    labelKey: 'Rejected',
    className:
      'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400',
  },
}

const PAYEE_LABELS: Record<RefundPayeeType, string> = {
  alipay: 'Alipay',
  wechat: 'WeChat',
  bank: 'Bank card',
  other: 'Other',
}

function CopyableText({
  value,
  className,
}: {
  value: string
  className?: string
}) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard({ notify: false })
  if (!value) return <span className='text-muted-foreground'>—</span>
  return (
    <span className={cn('inline-flex items-center gap-1', className)}>
      <span className='break-all'>{value}</span>
      <button
        type='button'
        onClick={async () => {
          if (await copyToClipboard(value)) {
            toast.success(t('Copied to clipboard'))
          }
        }}
        className='text-muted-foreground hover:text-foreground'
        aria-label={t('Copy')}
      >
        <Copy className='h-3 w-3' />
      </button>
    </span>
  )
}

type Props = {
  ticket: Ticket
  refund: TicketRefund
  admin: boolean
  onChanged: () => Promise<void> | void
}

export function RefundDetailCard({ ticket, refund, admin, onChanged }: Props) {
  const { t } = useTranslation()
  const isPending = refund.refund_status === REFUND_STATUS.PENDING
  // eflow: 客服 (role>=5) 只能 write_off 批准 + 驳回；subtract/override 属于资金调账，
  // 仅 admin 以上可见可改。后端 controller/ticket.go::UpdateRefundStatus 会兜底拒绝。
  const isAdmin = useIsAdmin()

  const [resolveOpen, setResolveOpen] = useState(false)
  const [rejectOpen, setRejectOpen] = useState(false)
  const [resolveLoading, setResolveLoading] = useState(false)
  const [rejectLoading, setRejectLoading] = useState(false)
  const [rejectReason, setRejectReason] = useState('')

  const [quotaMode, setQuotaMode] = useState<RefundQuotaMode>('write_off')
  const [customAmountStr, setCustomAmountStr] = useState('')
  const [targetUserQuota, setTargetUserQuota] = useState<number | null>(null)
  const [targetQuotaLoading, setTargetQuotaLoading] = useState(false)

  const targetUserId = ticket.user_id || refund.user_id || 0

  useEffect(() => {
    if (!resolveOpen) return
    setQuotaMode('write_off')
    setCustomAmountStr('')
    if (!targetUserId) {
      setTargetUserQuota(null)
      return
    }
    let active = true
    setTargetQuotaLoading(true)
    fetchUserQuota(targetUserId)
      .then((q) => {
        if (!active) return
        setTargetUserQuota(q)
      })
      .finally(() => {
        if (active) setTargetQuotaLoading(false)
      })
    return () => {
      active = false
    }
  }, [resolveOpen, targetUserId])

  const customAmountNum = Number(customAmountStr)
  const parsedCustomQuota = useMemo(() => {
    const trimmed = customAmountStr.trim()
    if (!trimmed) return null
    if (!Number.isFinite(customAmountNum) || customAmountNum < 0) return null
    return displayAmountToQuota(customAmountNum)
  }, [customAmountStr, customAmountNum])

  const frozenQ = refund.frozen_quota || 0

  const isResolveValid = useMemo(() => {
    if (quotaMode === 'write_off') return true
    if (parsedCustomQuota === null) return false
    if (quotaMode === 'subtract') {
      if (parsedCustomQuota <= 0) return false
      if (targetUserQuota !== null) {
        const available = targetUserQuota + frozenQ
        if (parsedCustomQuota > available) return false
      }
      return true
    }
    return parsedCustomQuota >= 0
  }, [quotaMode, parsedCustomQuota, targetUserQuota, frozenQ])

  const resolvePreview = useMemo(() => {
    if (quotaMode === 'subtract') {
      if (parsedCustomQuota === null || parsedCustomQuota <= 0) {
        return t('Enter a positive subtract amount')
      }
      if (targetUserQuota === null) {
        return t('Will subtract {{amount}} from unfrozen balance', {
          amount: formatQuotaWithCurrency(parsedCustomQuota),
        })
      }
      const after = targetUserQuota + frozenQ - parsedCustomQuota
      if (after < 0) {
        return t('Subtract amount exceeds unfrozen balance')
      }
      return `${formatQuotaWithCurrency(targetUserQuota + frozenQ)} − ${formatQuotaWithCurrency(parsedCustomQuota)} = ${formatQuotaWithCurrency(after)}`
    }
    if (quotaMode === 'override') {
      if (parsedCustomQuota === null) {
        return t('Enter the final balance (0 allowed)')
      }
      if (targetUserQuota === null) {
        return t('Final balance will be set to {{amount}}', {
          amount: formatQuotaWithCurrency(parsedCustomQuota),
        })
      }
      return `${formatQuotaWithCurrency(targetUserQuota + frozenQ)} → ${formatQuotaWithCurrency(parsedCustomQuota)}`
    }
    return ''
  }, [quotaMode, parsedCustomQuota, targetUserQuota, frozenQ, t])

  const handleResolve = async () => {
    if (!isResolveValid) return
    setResolveLoading(true)
    try {
      let payload: Parameters<typeof updateRefundStatus>[1]
      if (quotaMode === 'subtract' && parsedCustomQuota != null) {
        payload = {
          refund_status: REFUND_STATUS.REFUNDED,
          quota_mode: 'subtract',
          actual_refund_quota: parsedCustomQuota,
        }
      } else if (quotaMode === 'override' && parsedCustomQuota != null) {
        payload = {
          refund_status: REFUND_STATUS.REFUNDED,
          quota_mode: 'override',
          actual_refund_quota: parsedCustomQuota,
        }
      } else {
        payload = {
          refund_status: REFUND_STATUS.REFUNDED,
          quota_mode: 'write_off',
        }
      }
      await updateRefundStatus(ticket.id, payload)
      toast.success(t('Refund completed'))
      setResolveOpen(false)
      await onChanged()
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to update refund status')
      )
    } finally {
      setResolveLoading(false)
    }
  }

  const handleReject = async () => {
    setRejectLoading(true)
    try {
      const reasonText = rejectReason.trim()
      if (reasonText) {
        await postTicketReply(
          ticket.id,
          `${t('Rejection reason')}:\n${reasonText}`,
          true,
          []
        )
      }
      await updateRefundStatus(ticket.id, {
        refund_status: REFUND_STATUS.REJECTED,
      })
      toast.success(t('Refund rejected and quota unfrozen'))
      setRejectOpen(false)
      setRejectReason('')
      await onChanged()
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to update refund status')
      )
    } finally {
      setRejectLoading(false)
    }
  }

  const statusMeta = REFUND_STATUS_META[refund.refund_status]
  const requestedAmount = formatQuotaWithCurrency(refund.refund_quota)
  const frozenAmount = formatQuotaWithCurrency(
    refund.frozen_quota || refund.refund_quota
  )
  const snapshotAmount = formatQuotaWithCurrency(refund.user_quota_snapshot)

  return (
    <div className='bg-card rounded-2xl border p-4 sm:p-5'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex items-center gap-2'>
          <h3 className='text-base font-semibold'>{t('Refund details')}</h3>
          {statusMeta && (
            <Badge
              variant='outline'
              className={cn('font-normal', statusMeta.className)}
            >
              {t(statusMeta.labelKey)}
            </Badge>
          )}
        </div>
        {admin && isPending && (
          <div className='flex flex-wrap gap-2'>
            <Button
              type='button'
              variant='outline'
              className='text-destructive hover:text-destructive'
              onClick={() => {
                setRejectReason('')
                setRejectOpen(true)
              }}
            >
              {t('Reject and unfreeze')}
            </Button>
            <Button type='button' onClick={() => setResolveOpen(true)}>
              {t('Complete refund')}
            </Button>
          </div>
        )}
      </div>

      {/* Highlight cards */}
      <div className='mt-4 grid grid-cols-1 gap-3 md:grid-cols-3'>
        <div className='bg-primary/5 rounded-xl p-4'>
          <div className='text-muted-foreground text-xs'>
            {t('Requested refund amount')}
          </div>
          <div className='text-primary mt-1 text-xl font-semibold'>
            {requestedAmount}
          </div>
        </div>
        <div
          className={cn(
            'rounded-xl p-4',
            isPending ? 'bg-amber-500/10' : 'bg-muted/40'
          )}
        >
          <div className='text-muted-foreground text-xs'>
            {isPending ? t('Currently frozen') : t('Frozen at submission')}
          </div>
          <div
            className={cn(
              'mt-1 text-lg font-semibold',
              isPending && 'text-amber-700 dark:text-amber-400'
            )}
          >
            {frozenAmount}
          </div>
          <div className='text-muted-foreground mt-0.5 text-xs'>
            {isPending
              ? t('Already deducted from the user balance')
              : t('Freeze released after the ticket closed')}
          </div>
        </div>
        <div className='bg-muted/40 rounded-xl p-4'>
          <div className='text-muted-foreground text-xs'>
            {t('Available at submission')}
          </div>
          <div className='mt-1 text-lg font-semibold'>{snapshotAmount}</div>
        </div>
      </div>

      {/* Payee details */}
      <div className='mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2'>
        <div>
          <div className='text-muted-foreground text-xs'>{t('Payee type')}</div>
          <div className='mt-0.5 text-sm'>
            {t(PAYEE_LABELS[refund.payee_type] ?? refund.payee_type)}
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>{t('Payee name')}</div>
          <div className='mt-0.5 text-sm'>
            <CopyableText value={refund.payee_name} />
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>
            {t('Payee account')}
          </div>
          <div className='mt-0.5 text-sm'>
            <CopyableText value={refund.payee_account} />
          </div>
        </div>
        {refund.payee_bank && (
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Bank name')}
            </div>
            <div className='mt-0.5 text-sm'>
              <CopyableText value={refund.payee_bank} />
            </div>
          </div>
        )}
        <div>
          <div className='text-muted-foreground text-xs'>{t('Contact')}</div>
          <div className='mt-0.5 text-sm'>
            <CopyableText value={refund.contact} />
          </div>
        </div>
      </div>

      {/* Reason */}
      <div className='mt-4'>
        <div className='text-muted-foreground text-xs'>
          {t('Refund reason')}
        </div>
        <div className='bg-muted/40 mt-1 whitespace-pre-wrap rounded-lg p-3 text-sm'>
          {refund.reason || (
            <span className='text-muted-foreground'>
              {t('No reason provided')}
            </span>
          )}
        </div>
      </div>

      {/* Timeline */}
      <div className='text-muted-foreground mt-3 flex flex-wrap gap-x-6 gap-y-1 text-xs'>
        <span>
          {t('Submitted at')}: {formatTicketTimestamp(refund.created_time)}
        </span>
        {refund.processed_time > 0 && (
          <span>
            {t('Processed at')}: {formatTicketTimestamp(refund.processed_time)}
          </span>
        )}
      </div>

      {/* Resolve dialog */}
      <Dialog open={resolveOpen} onOpenChange={setResolveOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>{t('Complete refund')}</DialogTitle>
            <DialogDescription>
              {isAdmin
                ? t(
                    'Choose how the frozen quota should be handled. You can write it off, subtract a custom amount, or override the final balance directly.'
                  )
                : t(
                    'Approve the refund and write off the frozen quota. Custom subtract / override modes are admin-only.'
                  )}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-4'>
            <RadioGroup
              value={quotaMode}
              onValueChange={(v) => setQuotaMode(v as RefundQuotaMode)}
              className='space-y-2'
            >
              <Label
                htmlFor='qm-write-off'
                className='flex cursor-pointer items-start gap-3 rounded-md border p-3'
              >
                <RadioGroupItem id='qm-write-off' value='write_off' />
                <div className='space-y-0.5'>
                  <div className='text-sm font-medium'>
                    {t('Write off (default)')}
                  </div>
                  <div className='text-muted-foreground text-xs'>
                    {t('Treat the frozen amount as fully refunded; do not touch the user balance.')}
                  </div>
                </div>
              </Label>
              {isAdmin && (
                <>
                  <Label
                    htmlFor='qm-subtract'
                    className='flex cursor-pointer items-start gap-3 rounded-md border p-3'
                  >
                    <RadioGroupItem id='qm-subtract' value='subtract' />
                    <div className='space-y-0.5'>
                      <div className='text-sm font-medium'>
                        {t('Subtract custom amount')}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {t('Unfreeze first, then subtract the entered amount from the user balance.')}
                      </div>
                    </div>
                  </Label>
                  <Label
                    htmlFor='qm-override'
                    className='flex cursor-pointer items-start gap-3 rounded-md border p-3'
                  >
                    <RadioGroupItem id='qm-override' value='override' />
                    <div className='space-y-0.5'>
                      <div className='text-sm font-medium'>
                        {t('Override final balance')}
                      </div>
                      <div className='text-muted-foreground text-xs'>
                        {t('Unfreeze first, then set the final balance to the entered value (0 allowed).')}
                      </div>
                    </div>
                  </Label>
                </>
              )}
            </RadioGroup>

            {isAdmin && (quotaMode === 'subtract' || quotaMode === 'override') && (
              <div className='space-y-1.5'>
                <Label>
                  {quotaMode === 'subtract'
                    ? t('Subtract amount')
                    : t('Final balance')}
                </Label>
                <Input
                  type='number'
                  min={0}
                  step='0.01'
                  value={customAmountStr}
                  onChange={(e) => setCustomAmountStr(e.target.value)}
                  placeholder={t('Amount in displayed currency')}
                />
                <p className='text-muted-foreground text-xs'>
                  {targetQuotaLoading
                    ? t('Loading user balance…')
                    : resolvePreview}
                </p>
              </div>
            )}
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setResolveOpen(false)}
              disabled={resolveLoading}
            >
              {t('Cancel')}
            </Button>
            <Button
              onClick={handleResolve}
              disabled={!isResolveValid || resolveLoading}
            >
              {t('Confirm refund')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reject dialog */}
      <Dialog open={rejectOpen} onOpenChange={setRejectOpen}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Reject refund?')}</DialogTitle>
            <DialogDescription>
              {t('The frozen quota will be returned to the user.')}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-1.5'>
            <Label>
              {t('Optional rejection reason')}
            </Label>
            <Textarea
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              rows={4}
              placeholder={t('Reason will be posted as a reply to the user')}
            />
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setRejectOpen(false)}
              disabled={rejectLoading}
            >
              {t('Cancel')}
            </Button>
            <Button
              variant='destructive'
              onClick={handleReject}
              disabled={rejectLoading}
            >
              {t('Reject and unfreeze')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
