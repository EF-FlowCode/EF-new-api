import { useMemo, useState } from 'react'
import { ScissorsIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { estimateConversationUsage, trimConversation } from '../../lib'
import type { Message } from '../../types'

// 软阈值只用于变色提示：各模型真实上下文窗口未知，不做硬上限
const SOFT_TOKEN_LIMIT = 32_000
const KEEP_RECENT_MESSAGES = 10

type PlaygroundContextUsageProps = {
  disabled?: boolean
  messages: Message[]
  onTrim: (messages: Message[]) => void
  systemPrompt: string
}

export function PlaygroundContextUsage({
  disabled,
  messages,
  onTrim,
  systemPrompt,
}: PlaygroundContextUsageProps) {
  const { t } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)
  const usage = useMemo(
    () => estimateConversationUsage(messages, systemPrompt),
    [messages, systemPrompt]
  )

  if (messages.length === 0) return null

  const isOverSoftLimit = usage.tokens >= SOFT_TOKEN_LIMIT
  const canTrim = messages.length > KEEP_RECENT_MESSAGES

  return (
    <div className='text-muted-foreground -mt-2 flex min-h-7 items-center justify-end gap-2 px-2 text-xs'>
      <span
        className={cn(
          'tabular-nums',
          isOverSoftLimit && 'text-amber-600 dark:text-amber-500'
        )}
        title={t(
          'Rough estimate for reference only; images and PDFs are not included.'
        )}
      >
        {t('Context ≈ {{tokens}} tokens', {
          tokens: usage.tokens.toLocaleString(),
        })}
        {' · '}
        {t('{{count}} messages', { count: usage.messages })}
        {usage.files > 0 &&
          ` · ${t('{{count}} files', { count: usage.files })}`}
      </span>
      {canTrim && (
        <Button
          className='h-6 gap-1 px-2 text-xs'
          disabled={disabled}
          onClick={() => setConfirmOpen(true)}
          size='sm'
          variant='ghost'
        >
          <ScissorsIcon className='size-3' />
          {t('Trim')}
        </Button>
      )}

      <ConfirmDialog
        confirmText={t('Trim')}
        desc={t(
          'Older messages will be removed. System messages and the most recent {{count}} messages are kept.',
          { count: KEEP_RECENT_MESSAGES }
        )}
        handleConfirm={() => {
          onTrim(trimConversation(messages, KEEP_RECENT_MESSAGES))
          setConfirmOpen(false)
        }}
        onOpenChange={setConfirmOpen}
        open={confirmOpen}
        title={t('Trim conversation?')}
      />
    </div>
  )
}
