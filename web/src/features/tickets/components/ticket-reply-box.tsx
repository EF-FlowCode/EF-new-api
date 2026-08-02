import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Send } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { useTicketAttachments } from '../hooks/use-ticket-attachments'
import { AttachmentPicker } from './attachment-picker'

type Props = {
  disabled?: boolean
  loading?: boolean
  onSubmit: (content: string, attachmentIds: number[]) => Promise<boolean | void>
  placeholder?: string
  submitLabel?: string
}

/**
 * Ticket reply: text + attachment picker (clipboard paste / file picker).
 */
export function TicketReplyBox({
  disabled = false,
  loading = false,
  onSubmit,
  placeholder,
  submitLabel,
}: Props) {
  const { t } = useTranslation()
  const [content, setContent] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const attachmentState = useTicketAttachments()

  const canReply =
    !disabled &&
    (content.trim().length > 0 || attachmentState.attachments.length > 0)

  const handleSubmit = async () => {
    if (!canReply || loading || submitting || attachmentState.uploading) return
    setSubmitting(true)
    try {
      const ids = attachmentState.attachments.map((a) => a.id)
      const ok = await onSubmit(content.trim(), ids)
      if (ok) {
        setContent('')
        attachmentState.reset()
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      className='bg-card rounded-2xl border p-4 sm:p-5'
      onPasteCapture={(e) => {
        if (disabled || loading) return
        attachmentState.handlePaste(e)
      }}
    >
      <div className='mb-3 space-y-1'>
        <h4 className='text-base font-semibold'>{t('Reply')}</h4>
        {disabled && (
          <p className='text-muted-foreground text-xs'>
            {t('Ticket is closed. To continue, ask an admin to reopen it.')}
          </p>
        )}
      </div>
      <Textarea
        value={content}
        onChange={(e) => setContent(e.target.value)}
        rows={5}
        maxLength={5000}
        disabled={disabled || loading}
        placeholder={
          placeholder ??
          t('Type your reply (Ctrl/⌘+V to paste screenshots or files)')
        }
      />
      <div className='mt-3'>
        <AttachmentPicker
          state={attachmentState}
          disabled={disabled || loading}
        />
      </div>
      <div className='mt-3 flex justify-end'>
        <Button
          onClick={handleSubmit}
          disabled={
            disabled ||
            !canReply ||
            submitting ||
            loading ||
            attachmentState.uploading
          }
        >
          <Send className='mr-1.5 h-3.5 w-3.5' />
          {submitLabel ?? t('Send reply')}
        </Button>
      </div>
    </div>
  )
}
