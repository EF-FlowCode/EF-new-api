import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { displayAmountToQuota, formatQuotaWithCurrency } from '@/lib/currency'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { createRefundTicket, createTicket } from '../api'
import { useTicketAttachments } from '../hooks/use-ticket-attachments'
import { TICKET_PRIORITY, type TicketPriority, type TicketType } from '../types'
import { AttachmentPicker } from './attachment-picker'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: () => void
}

const PRIORITY_OPTIONS: Array<{ value: TicketPriority; labelKey: string }> = [
  { value: TICKET_PRIORITY.HIGH, labelKey: 'High priority' },
  { value: TICKET_PRIORITY.NORMAL, labelKey: 'Normal priority' },
  { value: TICKET_PRIORITY.LOW, labelKey: 'Low priority' },
]

// eflow: 退款只走支付宝。
//   - 微信大额必须加好友再转账，沟通成本高；
//   - 银行卡和"其他"过去几乎没人选；
//   - 只留支付宝意味着付款链路统一为「实名 + 手机号」，省得审核逐张工单还要判断渠道。
// 旧工单（数据库里 payee_type=wechat / bank / other）的展示仍由 refund-detail-card 兜住，不影响历史数据。

type TabValue = Exclude<TicketType, 'invoice'>

// ============================================================================
// General ticket form
// ============================================================================

function GeneralTicketForm({
  open,
  submitting,
  onCreated,
  onClose,
  setSubmitting,
}: {
  open: boolean
  submitting: boolean
  onCreated: () => void
  onClose: () => void
  setSubmitting: (v: boolean) => void
}) {
  const { t } = useTranslation()
  const [subject, setSubject] = useState('')
  const [content, setContent] = useState('')
  const [priority, setPriority] = useState<TicketPriority>(
    TICKET_PRIORITY.NORMAL
  )
  const attachmentState = useTicketAttachments()

  useEffect(() => {
    if (!open) {
      setSubject('')
      setContent('')
      setPriority(TICKET_PRIORITY.NORMAL)
      attachmentState.discardAll()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const canSubmit = subject.trim().length > 0 && content.trim().length > 0

  const submit = async () => {
    if (!canSubmit || submitting || attachmentState.uploading) return
    setSubmitting(true)
    try {
      await createTicket({
        type: 'general',
        subject: subject.trim(),
        priority,
        content: content.trim(),
        attachment_ids: attachmentState.attachments.map((a) => a.id),
      })
      toast.success(t('Ticket created'))
      attachmentState.reset()
      onCreated()
      onClose()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to create ticket'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <div className='space-y-4'>
        <div className='space-y-1.5'>
          <Label>{t('Subject')}</Label>
          <Input
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder={t('A short summary')}
            maxLength={200}
          />
        </div>
        <div className='space-y-1.5'>
          <Label>{t('Priority')}</Label>
          <Select
            value={String(priority)}
            onValueChange={(v) => setPriority(Number(v) as TicketPriority)}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {PRIORITY_OPTIONS.map((opt) => (
                <SelectItem key={opt.value} value={String(opt.value)}>
                  {t(opt.labelKey)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div
          className='space-y-1.5'
          onPasteCapture={(e) => attachmentState.handlePaste(e)}
        >
          <Label>{t('Description')}</Label>
          <Textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            rows={6}
            maxLength={5000}
            placeholder={t('Describe the issue in detail')}
          />
          <AttachmentPicker state={attachmentState} disabled={submitting} />
        </div>
      </div>
      <DialogFooter>
        <Button variant='outline' onClick={onClose} disabled={submitting}>
          {t('Cancel')}
        </Button>
        <Button onClick={submit} disabled={!canSubmit || submitting}>
          {t('Submit')}
        </Button>
      </DialogFooter>
    </>
  )
}

// ============================================================================
// Refund ticket form
// ============================================================================

function RefundTicketForm({
  open,
  submitting,
  setSubmitting,
  onCreated,
  onClose,
}: {
  open: boolean
  submitting: boolean
  setSubmitting: (v: boolean) => void
  onCreated: () => void
  onClose: () => void
}) {
  const { t } = useTranslation()
  const userEmail = useAuthStore((s) => s.auth.user?.email) ?? ''
  const [subject, setSubject] = useState('')
  const [priority, setPriority] = useState<TicketPriority>(
    TICKET_PRIORITY.NORMAL
  )
  const [refundAmountStr, setRefundAmountStr] = useState('')
  // eflow: 现在只支持支付宝退款。payeeType 锁死 'alipay'、payeeBank 永远空串，
  // 不再做选择器；payeeName 用作"支付宝实名"，payeeAccount 用作"支付宝绑定手机号"。
  const [payeeName, setPayeeName] = useState('')
  const [payeeAccount, setPayeeAccount] = useState('')
  // eflow: 联系字段统一改成"联系邮箱"，type=email，默认填用户注册邮箱。原"phone or
  // email"现实里 100% 都是邮箱，统一字段省得审核时还得判断。
  const [contact, setContact] = useState(userEmail)
  const [reason, setReason] = useState('')
  const [userQuota, setUserQuota] = useState<number | null>(null)

  useEffect(() => {
    if (!open) {
      setSubject('')
      setPriority(TICKET_PRIORITY.NORMAL)
      setRefundAmountStr('')
      setPayeeName('')
      setPayeeAccount('')
      setContact(userEmail)
      setReason('')
      setUserQuota(null)
      return
    }
    setContact(userEmail)
    // Load user's available quota for preview only.
    api
      .get('/api/user/self')
      .then((res) => {
        if (res.data?.success) {
          setUserQuota(Number(res.data?.data?.quota || 0))
        }
      })
      .catch(() => {
        /* informational only */
      })
    // userEmail 只用于 reset/init 时取快照，变更不需要 re-trigger 此 effect
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const refundAmountNum = Number(refundAmountStr)
  const refundQuota = Number.isFinite(refundAmountNum)
    ? displayAmountToQuota(refundAmountNum)
    : 0
  const exceedsQuota = userQuota != null && refundQuota > userQuota
  const amountValid =
    Number.isFinite(refundAmountNum) && refundAmountNum > 0 && !exceedsQuota
  // eflow: 支付宝绑定手机号校验 —— 1[3-9] 开头 11 位数字。允许中间有空格便于粘贴，但提交前 strip。
  const payeeAccountDigits = payeeAccount.replace(/\s+/g, '')
  const phoneValid = /^1[3-9]\d{9}$/.test(payeeAccountDigits)

  const canSubmit =
    subject.trim().length > 0 &&
    payeeName.trim().length > 0 &&
    phoneValid &&
    contact.trim().length > 0 &&
    amountValid

  const submit = async () => {
    if (!canSubmit || submitting) return
    setSubmitting(true)
    try {
      await createRefundTicket({
        subject: subject.trim(),
        priority,
        refund_quota: refundQuota,
        payee_type: 'alipay',
        payee_name: payeeName.trim(),
        payee_account: payeeAccountDigits,
        contact: contact.trim(),
        reason: reason.trim() || subject.trim(),
      })
      toast.success(t('Refund ticket created; quota frozen'))
      try {
        window.dispatchEvent(new CustomEvent('user-quota-changed'))
      } catch {
        /* noop */
      }
      onCreated()
      onClose()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to create ticket'))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <>
      <div className='space-y-4'>
        <div className='space-y-1.5'>
          <Label>{t('Subject')}</Label>
          <Input
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder={t('A short summary')}
            maxLength={200}
          />
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='space-y-1.5'>
            <Label>{t('Priority')}</Label>
            <Select
              value={String(priority)}
              onValueChange={(v) => setPriority(Number(v) as TicketPriority)}
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PRIORITY_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={String(opt.value)}>
                    {t(opt.labelKey)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1.5'>
            <Label>{t('Refund amount')}</Label>
            <Input
              type='number'
              min={0}
              step='0.01'
              value={refundAmountStr}
              onChange={(e) => setRefundAmountStr(e.target.value)}
              placeholder={t('Amount in displayed currency')}
            />
            <p className='text-muted-foreground text-xs'>
              {userQuota != null
                ? t('Available: {{amount}}', {
                    amount: formatQuotaWithCurrency(userQuota),
                  })
                : t('Loading available quota…')}
              {amountValid && refundQuota > 0 && (
                <>
                  {' '}
                  ·{' '}
                  {t('Will freeze: {{amount}}', {
                    amount: formatQuotaWithCurrency(refundQuota),
                  })}
                </>
              )}
            </p>
            {exceedsQuota && (
              <p className='text-destructive text-xs'>
                {t('Refund amount exceeds available quota')}
              </p>
            )}
          </div>
        </div>
        <div className='grid gap-3 sm:grid-cols-2'>
          <div className='space-y-1.5'>
            <Label>{t('Alipay real name')}</Label>
            <Input
              value={payeeName}
              onChange={(e) => setPayeeName(e.target.value)}
              placeholder={t('Name on the Alipay account')}
              maxLength={128}
            />
          </div>
          <div className='space-y-1.5'>
            <Label>{t('Alipay phone number')}</Label>
            <Input
              type='tel'
              inputMode='numeric'
              value={payeeAccount}
              onChange={(e) => setPayeeAccount(e.target.value)}
              placeholder={t('11-digit mobile bound to Alipay')}
              maxLength={32}
            />
            {payeeAccount.trim().length > 0 && !phoneValid && (
              <p className='text-destructive text-xs'>
                {t(
                  'Please enter a valid 11-digit mainland China mobile number'
                )}
              </p>
            )}
          </div>
        </div>
        <p className='text-muted-foreground -mt-2 text-xs'>
          {t(
            'Refunds are issued via Alipay only. Large transfers fail without a real name match, so both fields are required.'
          )}
        </p>
        <div className='space-y-1.5'>
          <Label>{t('Contact email')}</Label>
          <Input
            type='email'
            value={contact}
            onChange={(e) => setContact(e.target.value)}
            placeholder={t('Defaults to your registered email')}
            maxLength={128}
          />
        </div>
        <div className='space-y-1.5'>
          <Label>{t('Refund reason')}</Label>
          <Textarea
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            rows={4}
            maxLength={5000}
            placeholder={t('Optional: explain why a refund is needed')}
          />
        </div>
      </div>
      <DialogFooter>
        <Button variant='outline' onClick={onClose} disabled={submitting}>
          {t('Cancel')}
        </Button>
        <Button onClick={submit} disabled={!canSubmit || submitting}>
          {t('Submit refund request')}
        </Button>
      </DialogFooter>
    </>
  )
}

// ============================================================================
// Wrapper dialog with tabs
// ============================================================================

export function CreateTicketDialog({ open, onOpenChange, onCreated }: Props) {
  const { t } = useTranslation()
  const [tab, setTab] = useState<TabValue>('general')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    // 关闭对话框时重置 tab 到 'general'
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (!open) setTab('general')
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>{t('New ticket')}</DialogTitle>
          <DialogDescription>
            {t(
              'Submit a question, request, or refund. Invoice requests live on the topup history page.'
            )}
          </DialogDescription>
        </DialogHeader>
        <Tabs value={tab} onValueChange={(v) => setTab(v as TabValue)}>
          <TabsList className='grid w-full grid-cols-2'>
            <TabsTrigger value='general'>{t('General')}</TabsTrigger>
            <TabsTrigger value='refund'>{t('Refund')}</TabsTrigger>
          </TabsList>
          <TabsContent value='general' className='space-y-4'>
            <GeneralTicketForm
              open={open && tab === 'general'}
              submitting={submitting}
              setSubmitting={setSubmitting}
              onCreated={onCreated}
              onClose={() => onOpenChange(false)}
            />
          </TabsContent>
          <TabsContent value='refund' className='space-y-4'>
            <RefundTicketForm
              open={open && tab === 'refund'}
              submitting={submitting}
              setSubmitting={setSubmitting}
              onCreated={onCreated}
              onClose={() => onOpenChange(false)}
            />
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}
