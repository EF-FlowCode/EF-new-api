import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { canSwitchVersions, getActiveVersionIndex } from '../../lib'
import type { Message } from '../../types'

type MessageVersionSwitcherProps = {
  disabled?: boolean
  message: Message
  onSwitch?: (message: Message, versionIndex: number) => void
}

/**
 * "‹ 2 / 3 ›" for answers that were regenerated.
 */
export function MessageVersionSwitcher({
  disabled = false,
  message,
  onSwitch,
}: MessageVersionSwitcherProps) {
  const { t } = useTranslation()
  if (!onSwitch || !canSwitchVersions(message)) return null

  const activeIndex = getActiveVersionIndex(message)
  const total = message.versions.length

  return (
    <div
      aria-label={t('Answer versions')}
      className='text-muted-foreground flex items-center gap-0.5 text-xs'
      role='group'
    >
      <Button
        aria-label={t('Previous version')}
        className='size-7'
        disabled={disabled || activeIndex === 0}
        onClick={() => onSwitch(message, activeIndex - 1)}
        size='icon'
        variant='ghost'
      >
        <ChevronLeftIcon className='size-4' />
      </Button>
      <span className='min-w-9 text-center tabular-nums'>
        {activeIndex + 1} / {total}
      </span>
      <Button
        aria-label={t('Next version')}
        className='size-7'
        disabled={disabled || activeIndex === total - 1}
        onClick={() => onSwitch(message, activeIndex + 1)}
        size='icon'
        variant='ghost'
      >
        <ChevronRightIcon className='size-4' />
      </Button>
    </div>
  )
}
