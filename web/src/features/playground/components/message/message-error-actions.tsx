import { Edit, RefreshCw, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { MessageActionButton } from './message-action-button'

type MessageErrorActionsProps = {
  disabled?: boolean
  onDelete?: () => void
  onEditPrompt?: () => void
  onRetry?: () => void
}

export function MessageErrorActions({
  disabled = false,
  onDelete,
  onEditPrompt,
  onRetry,
}: MessageErrorActionsProps) {
  const { t } = useTranslation()

  if (!onRetry && !onEditPrompt && !onDelete) {
    return null
  }

  return (
    <div className='flex flex-wrap items-center gap-0.5 pt-2'>
      {onRetry && (
        <MessageActionButton
          disabled={disabled}
          icon={RefreshCw}
          label={t('Retry')}
          onClick={onRetry}
        />
      )}

      {onEditPrompt && (
        <MessageActionButton
          disabled={disabled}
          icon={Edit}
          label={t('Edit')}
          onClick={onEditPrompt}
        />
      )}

      {onDelete && (
        <MessageActionButton
          disabled={disabled}
          icon={Trash2}
          label={t('Delete')}
          onClick={onDelete}
          variant='destructive'
        />
      )}
    </div>
  )
}
