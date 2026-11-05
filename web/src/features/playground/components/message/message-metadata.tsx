import type { TFunction } from 'i18next'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import type { MessageAlignment } from '../../lib'
import type { Message } from '../../types'

type MessageMetadataProps = {
  alignment: MessageAlignment
  message: Message
}

function formatMessageTime(timestamp?: number): string | undefined {
  if (typeof timestamp !== 'number' || !Number.isFinite(timestamp)) {
    return undefined
  }

  return new Intl.DateTimeFormat(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  }).format(new Date(timestamp))
}

function formatDuration(
  durationMs: number | undefined,
  t: TFunction
): string | undefined {
  if (typeof durationMs !== 'number' || !Number.isFinite(durationMs)) {
    return undefined
  }

  if (durationMs < 1000) {
    return t('{{value}}ms', { value: Math.max(1, Math.round(durationMs)) })
  }

  return t('{{value}}s', { value: (durationMs / 1000).toFixed(2) })
}

export function MessageMetadata(props: MessageMetadataProps) {
  const { t } = useTranslation()
  const messageTime = formatMessageTime(props.message.createdAt)
  const duration = formatDuration(props.message.durationMs, t)

  if (!messageTime && !duration) {
    return null
  }

  return (
    <div
      className={cn(
        'text-muted-foreground mt-1 flex min-h-4 items-center gap-1.5 text-[11px] leading-none',
        props.alignment === 'right' && 'justify-end'
      )}
    >
      {messageTime && <time>{messageTime}</time>}
      {duration && (
        <>
          {messageTime && <span aria-hidden='true'>·</span>}
          <span>{t('Response time: {{duration}}', { duration })}</span>
        </>
      )}
    </div>
  )
}
