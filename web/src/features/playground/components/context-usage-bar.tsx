import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { estimateConversationUsage } from '../lib'
import type { Message as MessageType } from '../types'

// 软阈值仅用于变色提示——各模型真实上下文窗口未知，不做硬上限。
const SOFT_LIMIT = 32000
const KEEP_RECENT = 10

interface ContextUsageBarProps {
  messages: MessageType[]
  disabled?: boolean
  onTrim: (next: MessageType[]) => void
}

export function ContextUsageBar({
  messages,
  disabled,
  onTrim,
}: ContextUsageBarProps) {
  const { t } = useTranslation()
  const usage = useMemo(() => estimateConversationUsage(messages), [messages])

  if (messages.length === 0) return null

  const over = usage.tokens >= SOFT_LIMIT
  const canTrim = messages.length > KEEP_RECENT

  const handleTrim = () => {
    if (!canTrim) return
    const keepFrom = messages.length - KEEP_RECENT
    // 保留所有 system 提示 + 最近 KEEP_RECENT 条
    const kept = messages.filter((m, i) => m.from === 'system' || i >= keepFrom)
    if (
      typeof window !== 'undefined' &&
      !window.confirm(
        t(
          'Trim older messages? Keeps system prompts and the most recent {{n}} messages.',
          { n: KEEP_RECENT }
        )
      )
    ) {
      return
    }
    onTrim(kept)
  }

  return (
    <div className='text-muted-foreground flex items-center justify-end gap-2 px-4 pb-1 text-xs'>
      <span className={cn(over && 'text-amber-600 dark:text-amber-500')}>
        {t('Context')}: ≈{usage.tokens.toLocaleString()} tokens ·{' '}
        {usage.messages} {t('msgs')}
        {usage.images > 0 ? ` · ${usage.images} ${t('imgs')}` : ''}
      </span>
      {canTrim && (
        <Button
          size='sm'
          variant='ghost'
          className='h-6 px-2 text-xs'
          disabled={disabled}
          onClick={handleTrim}
        >
          {t('Trim')}
        </Button>
      )}
    </div>
  )
}
