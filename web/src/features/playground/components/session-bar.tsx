import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { DEFAULT_SESSION_TITLE } from '../constants'
import { exportSession } from '../lib'
import type { Session } from '../types'

interface SessionBarProps {
  sessions: Session[]
  activeId: string
  disabled?: boolean
  onSwitch: (id: string) => void
  onCreate: () => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
}

export function SessionBar({
  sessions,
  activeId,
  disabled,
  onSwitch,
  onCreate,
  onRename,
  onDelete,
}: SessionBarProps) {
  const { t } = useTranslation()
  const active = sessions.find((s) => s.id === activeId)

  const handleRename = () => {
    if (!active || typeof window === 'undefined') return
    const next = window.prompt(t('Rename session'), active.title)
    if (next && next.trim()) onRename(active.id, next.trim())
  }

  const handleDelete = () => {
    if (!active || typeof window === 'undefined') return
    if (window.confirm(t('Delete this session? This cannot be undone.'))) {
      onDelete(active.id)
    }
  }

  return (
    <div className='flex flex-wrap items-center gap-2 border-b px-3 py-2'>
      <select
        className='bg-background h-8 w-full min-w-0 truncate rounded-md border px-2 text-sm'
        value={activeId}
        disabled={disabled}
        onChange={(e) => onSwitch(e.target.value)}
        aria-label={t('Sessions')}
      >
        {sessions.map((s) => (
          <option key={s.id} value={s.id}>
            {s.title === DEFAULT_SESSION_TITLE ? t('New Chat') : s.title}
          </option>
        ))}
      </select>
      <Button
        size='sm'
        variant='ghost'
        className='h-7 px-2 text-xs'
        onClick={onCreate}
        disabled={disabled}
      >
        {t('New chat')}
      </Button>
      <Button
        size='sm'
        variant='ghost'
        className='h-7 px-2 text-xs'
        onClick={handleRename}
        disabled={disabled || !active}
      >
        {t('Rename')}
      </Button>
      <Button
        size='sm'
        variant='ghost'
        className='h-7 px-2 text-xs'
        onClick={handleDelete}
        disabled={disabled || sessions.length <= 1}
      >
        {t('Delete')}
      </Button>
      <select
        className='bg-background h-7 rounded-md border px-1 text-xs'
        value=''
        disabled={disabled || !active}
        onChange={(e) => {
          const fmt = e.target.value as 'md' | 'json'
          if (active && (fmt === 'md' || fmt === 'json')) {
            exportSession(active, fmt)
          }
          e.currentTarget.value = ''
        }}
        aria-label={t('Export')}
      >
        <option value=''>{t('Export')}</option>
        <option value='md'>{t('Markdown')}</option>
        <option value='json'>{t('JSON')}</option>
      </select>
    </div>
  )
}
