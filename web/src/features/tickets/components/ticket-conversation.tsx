import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { File as FileIcon } from 'lucide-react'
import {
  formatTicketTimestamp,
  resolveRoleBadge,
} from '../lib/ticket-utils'
import type { TicketAttachment, TicketMessage } from '../types'

const humanSize = (bytes: number | undefined): string => {
  const n = Number(bytes) || 0
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

const isImageMime = (mime: string | undefined): boolean =>
  typeof mime === 'string' && mime.toLowerCase().startsWith('image/')

const attachmentUrl = (id: number, inline = false): string =>
  `/api/ticket/attachment/${id}${inline ? '?inline=1' : ''}`

type Props = {
  messages: TicketMessage[]
  currentUserId: number | undefined
  loading?: boolean
}

export function TicketConversation({
  messages,
  currentUserId,
  loading = false,
}: Props) {
  const { t } = useTranslation()
  const bottomRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth', block: 'end' })
  }, [messages.length])

  return (
    <div className='bg-card rounded-2xl border p-4 sm:p-5'>
      <div className='mb-4 flex items-center justify-between'>
        <h3 className='text-base font-semibold'>{t('Conversation')}</h3>
      </div>

      {messages.length === 0 ? (
        <div className='text-muted-foreground py-12 text-center text-sm'>
          {loading ? t('Loading…') : t('No messages yet')}
        </div>
      ) : (
        <div className='flex flex-col gap-3'>
          {messages.map((message) => (
            <MessageItem
              key={message.id}
              message={message}
              isMine={Number(message.user_id) === Number(currentUserId)}
            />
          ))}
          <div ref={bottomRef} />
        </div>
      )}
    </div>
  )
}

function MessageItem({
  message,
  isMine,
}: {
  message: TicketMessage
  isMine: boolean
}) {
  const { t } = useTranslation()
  const badge = resolveRoleBadge(message.role)
  const attachments: TicketAttachment[] = Array.isArray(message.attachments)
    ? message.attachments
    : []
  const images = attachments.filter((a) => isImageMime(a.mime_type))
  const files = attachments.filter((a) => !isImageMime(a.mime_type))

  return (
    <div className={cn('flex', isMine ? 'justify-end' : 'justify-start')}>
      <div
        className={cn(
          'w-full rounded-2xl border px-4 py-3 md:max-w-[78%]',
          isMine
            ? 'border-primary/30 bg-primary/5'
            : 'bg-muted/40 border-muted-foreground/10'
        )}
      >
        <div className='mb-2 flex items-start justify-between gap-3'>
          <div className='flex flex-wrap items-center gap-1.5'>
            <span className='text-sm font-semibold'>
              {message.username || t('Unknown user')}
            </span>
            <Badge variant='outline' className={cn('font-normal', badge.className)}>
              {t(badge.labelKey)}
            </Badge>
          </div>
          <span className='text-muted-foreground text-xs'>
            {formatTicketTimestamp(message.created_time)}
          </span>
        </div>

        {message.content && (
          <p className='whitespace-pre-wrap break-words text-sm'>
            {message.content}
          </p>
        )}

        {images.length > 0 && (
          <div className='mt-2 flex flex-wrap gap-2'>
            {images.map((img) => (
              <a
                key={img.id}
                href={attachmentUrl(img.id, true)}
                target='_blank'
                rel='noreferrer'
                className='overflow-hidden rounded-md border'
              >
                <img
                  src={attachmentUrl(img.id, true)}
                  alt={img.file_name}
                  className='h-28 w-28 object-cover'
                />
              </a>
            ))}
          </div>
        )}

        {files.length > 0 && (
          <div className='mt-2 flex flex-col gap-1'>
            {files.map((f) => (
              <a
                key={f.id}
                href={attachmentUrl(f.id)}
                target='_blank'
                rel='noreferrer'
                className='text-primary inline-flex items-center gap-1.5 text-xs no-underline hover:underline'
              >
                <FileIcon className='h-3 w-3' />
                <span className='truncate'>{f.file_name}</span>
                <span className='text-muted-foreground'>
                  ({humanSize(f.size)})
                </span>
              </a>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
