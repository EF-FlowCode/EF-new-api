import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Copy } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { cn } from '@/lib/utils'
import { updateInvoiceStatus } from '../api'
import { formatTicketTimestamp } from '../lib/ticket-utils'
import {
  INVOICE_STATUS,
  type EligibleTopUpOrder,
  type InvoiceStatus,
  type Ticket,
  type TicketInvoice,
} from '../types'

const INVOICE_STATUS_META: Record<
  InvoiceStatus,
  { labelKey: string; className: string }
> = {
  [INVOICE_STATUS.PENDING]: {
    labelKey: 'Pending issue',
    className:
      'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  },
  [INVOICE_STATUS.ISSUED]: {
    labelKey: 'Issued',
    className:
      'border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
  },
  [INVOICE_STATUS.REJECTED]: {
    labelKey: 'Rejected',
    className:
      'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400',
  },
}

function CopyableText({ value }: { value: string }) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard({ notify: false })
  if (!value) return <span className='text-muted-foreground'>—</span>
  return (
    <span className='inline-flex items-center gap-1'>
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
  invoice: TicketInvoice
  orders: EligibleTopUpOrder[]
  admin: boolean
  onChanged: () => Promise<void> | void
}

export function InvoiceDetailCard({
  ticket,
  invoice,
  orders,
  admin,
  onChanged,
}: Props) {
  const { t } = useTranslation()
  const [updating, setUpdating] = useState(false)
  const isPending = invoice.invoice_status === INVOICE_STATUS.PENDING
  const statusMeta = INVOICE_STATUS_META[invoice.invoice_status]

  const handleStatus = async (next: InvoiceStatus) => {
    setUpdating(true)
    try {
      await updateInvoiceStatus(ticket.id, next)
      toast.success(t('Invoice status updated'))
      await onChanged()
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to update invoice status')
      )
    } finally {
      setUpdating(false)
    }
  }

  return (
    <div className='bg-card rounded-2xl border p-4 sm:p-5'>
      <div className='flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex items-center gap-2'>
          <h3 className='text-base font-semibold'>{t('Invoice details')}</h3>
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
              onClick={() => handleStatus(INVOICE_STATUS.REJECTED)}
              disabled={updating}
            >
              {t('Reject invoice')}
            </Button>
            <Button
              type='button'
              onClick={() => handleStatus(INVOICE_STATUS.ISSUED)}
              disabled={updating}
            >
              {t('Mark as issued')}
            </Button>
          </div>
        )}
      </div>

      {admin && isPending && (
        <p className='text-muted-foreground mt-2 text-xs'>
          {t(
            'Reply with the invoice file as an attachment first, then mark as issued — the invoice will be emailed to the address filled in the request.'
          )}
        </p>
      )}

      <div className='mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2'>
        <div>
          <div className='text-muted-foreground text-xs'>
            {t('Company name')}
          </div>
          <div className='mt-0.5 text-sm'>
            <CopyableText value={invoice.company_name} />
          </div>
        </div>
        <div>
          <div className='text-muted-foreground text-xs'>
            {t('Tax number')}
          </div>
          <div className='mt-0.5 text-sm'>
            <CopyableText value={invoice.tax_number} />
          </div>
        </div>
        {invoice.bank_name && (
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Bank name')}
            </div>
            <div className='mt-0.5 text-sm'>
              <CopyableText value={invoice.bank_name} />
            </div>
          </div>
        )}
        {invoice.bank_account && (
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Bank account')}
            </div>
            <div className='mt-0.5 text-sm'>
              <CopyableText value={invoice.bank_account} />
            </div>
          </div>
        )}
        {invoice.company_address && (
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Company address')}
            </div>
            <div className='mt-0.5 text-sm'>
              <CopyableText value={invoice.company_address} />
            </div>
          </div>
        )}
        {invoice.company_phone && (
          <div>
            <div className='text-muted-foreground text-xs'>
              {t('Company phone')}
            </div>
            <div className='mt-0.5 text-sm'>
              <CopyableText value={invoice.company_phone} />
            </div>
          </div>
        )}
        <div className='sm:col-span-2'>
          <div className='text-muted-foreground text-xs'>
            {t('Email for invoice delivery')}
          </div>
          <div className='mt-0.5 text-sm'>
            <CopyableText value={invoice.email} />
          </div>
        </div>
      </div>

      <div className='mt-4'>
        <div className='text-muted-foreground text-xs'>
          {t('Total amount')}
        </div>
        <div className='text-primary mt-1 text-xl font-semibold'>
          ¥{(invoice.total_money ?? 0).toFixed(2)}
        </div>
      </div>

      {orders.length > 0 && (
        <div className='mt-4'>
          <div className='text-muted-foreground mb-1 text-xs'>
            {t('Included topup orders')}
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <table className='w-full text-sm'>
              <thead className='bg-muted/40 text-muted-foreground text-xs'>
                <tr>
                  <th className='px-3 py-2 text-left'>{t('Trade no.')}</th>
                  <th className='px-3 py-2 text-left'>{t('Payment')}</th>
                  <th className='px-3 py-2 text-right'>{t('Amount')}</th>
                  <th className='px-3 py-2 text-left'>{t('Created')}</th>
                </tr>
              </thead>
              <tbody>
                {orders.map((o) => (
                  <tr key={o.id} className='border-t'>
                    <td className='px-3 py-2 font-mono text-xs'>
                      {o.trade_no}
                    </td>
                    <td className='px-3 py-2 text-xs'>
                      {o.payment_method || '—'}
                    </td>
                    <td className='px-3 py-2 text-right text-rose-600 dark:text-rose-400'>
                      ¥{(o.money ?? 0).toFixed(2)}
                    </td>
                    <td className='text-muted-foreground px-3 py-2 text-xs'>
                      {formatTicketTimestamp(o.create_time)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <div className='text-muted-foreground mt-3 flex flex-wrap gap-x-6 gap-y-1 text-xs'>
        <span>
          {t('Submitted at')}: {formatTicketTimestamp(invoice.created_time)}
        </span>
        {invoice.issued_time > 0 && (
          <span>
            {t('Issued at')}: {formatTicketTimestamp(invoice.issued_time)}
          </span>
        )}
      </div>
    </div>
  )
}
