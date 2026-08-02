import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'
import {
  createInvoiceTicket,
  fetchEligibleInvoiceOrders,
} from '../api'
import { TICKET_PRIORITY, type EligibleTopUpOrder } from '../types'

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated?: () => void
}

function formatTimestamp(unixSec: number | undefined): string {
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

export function InvoiceTicketDialog({ open, onOpenChange, onCreated }: Props) {
  const { t } = useTranslation()
  const userEmail = useAuthStore((s) => s.auth.user?.email) ?? ''
  const [orders, setOrders] = useState<EligibleTopUpOrder[]>([])
  const [loadingOrders, setLoadingOrders] = useState(false)
  const [selected, setSelected] = useState<Set<number>>(new Set())

  // eflow: 中国式发票场景区分个人/企业。个人发票一般不要税号；后端配套删了
  // ErrTicketInvoiceTaxNumberEmpty 必填校验，前端按此切 visibility + 必填规则。
  const [invoiceType, setInvoiceType] = useState<'personal' | 'enterprise'>(
    'personal'
  )
  const [companyName, setCompanyName] = useState('')
  const [taxNumber, setTaxNumber] = useState('')
  const [bankName, setBankName] = useState('')
  const [bankAccount, setBankAccount] = useState('')
  const [companyAddress, setCompanyAddress] = useState('')
  const [companyPhone, setCompanyPhone] = useState('')
  const [email, setEmail] = useState('')
  const [content, setContent] = useState('')
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (!open) {
      setOrders([])
      setSelected(new Set())
      setInvoiceType('personal')
      setCompanyName('')
      setTaxNumber('')
      setBankName('')
      setBankAccount('')
      setCompanyAddress('')
      setCompanyPhone('')
      setEmail(userEmail)
      setContent('')
      return
    }
    setEmail(userEmail)
    setLoadingOrders(true)
    fetchEligibleInvoiceOrders()
      .then((data) => setOrders(data))
      .catch((e) =>
        toast.error(
          e instanceof Error
            ? e.message
            : t('Failed to load invoice eligible orders')
        )
      )
      .finally(() => setLoadingOrders(false))
    // userEmail 只用于 reset/init 时取快照，变更不需要 re-trigger 此 effect
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, t])

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const totalMoney = useMemo(() => {
    let sum = 0
    for (const o of orders) {
      if (selected.has(o.id)) sum += Number(o.money) || 0
    }
    return sum
  }, [orders, selected])

  const allSelected = orders.length > 0 && selected.size === orders.length
  const noneSelected = selected.size === 0

  const isEnterprise = invoiceType === 'enterprise'
  const canSubmit =
    !noneSelected &&
    companyName.trim().length > 0 &&
    (!isEnterprise || taxNumber.trim().length > 0) &&
    /\S+@\S+\.\S+/.test(email.trim())

  const submit = async () => {
    if (!canSubmit || submitting) return
    setSubmitting(true)
    try {
      const subject = t('Invoice request ({{n}} orders)', { n: selected.size })
      await createInvoiceTicket({
        subject,
        priority: TICKET_PRIORITY.NORMAL,
        content: content.trim() || subject,
        company_name: companyName.trim(),
        // 个人发票场景下后端允许税号为空（eflow 删了 TaxNumber 必填）；
        // 这里强制为空字符串以避免无意义的脏数据。
        tax_number: isEnterprise ? taxNumber.trim() : '',
        bank_name: bankName.trim() || undefined,
        bank_account: bankAccount.trim() || undefined,
        company_address: companyAddress.trim() || undefined,
        company_phone: companyPhone.trim() || undefined,
        email: email.trim(),
        topup_order_ids: Array.from(selected),
      })
      toast.success(t('Invoice ticket created'))
      onCreated?.()
      onOpenChange(false)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to create invoice ticket')
      )
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Request invoice')}</DialogTitle>
          <DialogDescription>
            {t(
              'Pick the topup orders to invoice and provide the billing information.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-5'>
          <section className='space-y-2'>
            <div className='flex items-center justify-between'>
              <h4 className='text-sm font-semibold'>{t('Eligible orders')}</h4>
              <div className='flex items-center gap-2'>
                <span className='text-muted-foreground text-xs'>
                  {t('Total: ¥{{amount}}', {
                    amount: totalMoney.toFixed(2),
                  })}
                </span>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={() =>
                    setSelected(
                      allSelected ? new Set() : new Set(orders.map((o) => o.id))
                    )
                  }
                  disabled={loadingOrders || orders.length === 0}
                >
                  {allSelected ? t('Deselect all') : t('Select all')}
                </Button>
              </div>
            </div>
            <div className='max-h-60 overflow-y-auto rounded-lg border'>
              {loadingOrders ? (
                <div className='space-y-2 p-3'>
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className='h-10 w-full' />
                  ))}
                </div>
              ) : orders.length === 0 ? (
                <div className='text-muted-foreground p-6 text-center text-sm'>
                  {t('No orders eligible for invoice')}
                </div>
              ) : (
                <ul className='divide-y'>
                  {orders.map((o) => {
                    const checked = selected.has(o.id)
                    return (
                      <li key={o.id}>
                        <button
                          type='button'
                          onClick={() => toggle(o.id)}
                          className={cn(
                            'hover:bg-accent/40 flex w-full items-center gap-3 px-3 py-2 text-left transition-colors',
                            checked && 'bg-primary/5'
                          )}
                        >
                          <Checkbox
                            checked={checked}
                            onCheckedChange={() => toggle(o.id)}
                            onClick={(e) => e.stopPropagation()}
                          />
                          <div className='min-w-0 flex-1'>
                            <div className='flex flex-wrap items-center gap-2'>
                              <span className='font-mono text-xs'>
                                {o.trade_no}
                              </span>
                              <span className='text-muted-foreground text-xs'>
                                {o.payment_method || '—'}
                              </span>
                              <span className='text-muted-foreground text-xs'>
                                {formatTimestamp(o.create_time)}
                              </span>
                            </div>
                          </div>
                          <span className='text-rose-600 dark:text-rose-400'>
                            ¥{(o.money ?? 0).toFixed(2)}
                          </span>
                        </button>
                      </li>
                    )
                  })}
                </ul>
              )}
            </div>
          </section>

          <section className='space-y-1.5'>
            <Label>{t('Invoice type')}</Label>
            <RadioGroup
              value={invoiceType}
              onValueChange={(v) =>
                setInvoiceType(v as 'personal' | 'enterprise')
              }
              className='flex gap-6'
            >
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='personal' id='inv-type-personal' />
                <Label
                  htmlFor='inv-type-personal'
                  className='cursor-pointer font-normal'
                >
                  {t('Personal')}
                </Label>
              </div>
              <div className='flex items-center gap-2'>
                <RadioGroupItem value='enterprise' id='inv-type-enterprise' />
                <Label
                  htmlFor='inv-type-enterprise'
                  className='cursor-pointer font-normal'
                >
                  {t('Enterprise')}
                </Label>
              </div>
            </RadioGroup>
          </section>

          <section className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5 sm:col-span-2'>
              <Label>
                {isEnterprise
                  ? t('Title (company full name)')
                  : t('Title (personal name)')}
                <span className='text-destructive'> *</span>
              </Label>
              <Input
                value={companyName}
                onChange={(e) => setCompanyName(e.target.value)}
                maxLength={255}
                placeholder={
                  isEnterprise ? t('Company full name') : t('Personal name')
                }
              />
            </div>
            {isEnterprise && (
              <div className='space-y-1.5 sm:col-span-2'>
                <Label>
                  {t('Tax number')}
                  <span className='text-destructive'> *</span>
                </Label>
                <Input
                  value={taxNumber}
                  onChange={(e) => setTaxNumber(e.target.value)}
                  maxLength={64}
                />
              </div>
            )}
            <div className='space-y-1.5'>
              <Label>{t('Bank name')}</Label>
              <Input
                value={bankName}
                onChange={(e) => setBankName(e.target.value)}
                maxLength={255}
              />
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Bank account')}</Label>
              <Input
                value={bankAccount}
                onChange={(e) => setBankAccount(e.target.value)}
                maxLength={128}
              />
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Company address')}</Label>
              <Input
                value={companyAddress}
                onChange={(e) => setCompanyAddress(e.target.value)}
                maxLength={512}
              />
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Company phone')}</Label>
              <Input
                value={companyPhone}
                onChange={(e) => setCompanyPhone(e.target.value)}
                maxLength={32}
              />
            </div>
            <div className='space-y-1.5 sm:col-span-2'>
              <Label>
                {t('Email for invoice delivery')}
                <span className='text-destructive'> *</span>
              </Label>
              <Input
                type='email'
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                maxLength={128}
                placeholder='you@example.com'
              />
            </div>
            <div className='space-y-1.5 sm:col-span-2'>
              <Label>{t('Notes')}</Label>
              <Textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                rows={3}
                maxLength={2000}
                placeholder={t('Optional notes for the support team')}
              />
            </div>
          </section>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={!canSubmit || submitting}>
            {t('Submit invoice request')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
