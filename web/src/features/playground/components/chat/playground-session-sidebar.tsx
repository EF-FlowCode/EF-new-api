import { useMemo, useState } from 'react'
import {
  DownloadIcon,
  MessageSquareIcon,
  MoreHorizontalIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  PencilIcon,
  PlusIcon,
  SearchIcon,
  Trash2Icon,
  XIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { DEFAULT_SESSION_TITLE } from '../../constants'
import {
  exportSession,
  getMessageContent,
  getSortedSessions,
  sessionMatchesQuery,
} from '../../lib'
import type { Session } from '../../types'

export type PlaygroundSessionActions = {
  onCreate: () => void
  onSwitch: (id: string) => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
  /** Called when a search result is opened, with the matching message keys. */
  onRevealMatches: (sessionId: string, messageKeys: string[]) => void
}

type PlaygroundSessionListProps = PlaygroundSessionActions & {
  sessions: Session[]
  activeSessionId: string
  disabled?: boolean
  onNavigate?: () => void
}

function formatSessionTime(timestamp: number): string {
  const date = new Date(timestamp)
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  if (date.toDateString() === now.toDateString()) {
    return `${pad(date.getHours())}:${pad(date.getMinutes())}`
  }
  const monthDay = `${pad(date.getMonth() + 1)}-${pad(date.getDate())}`
  return date.getFullYear() === now.getFullYear()
    ? monthDay
    : `${date.getFullYear()}-${monthDay}`
}

function getMatchingMessageKeys(session: Session, query: string): string[] {
  const q = query.trim().toLowerCase()
  if (!q) return []
  return session.messages
    .filter((message) => getMessageContent(message).toLowerCase().includes(q))
    .map((message) => message.key)
}

/**
 * Session list with search, rename, export and delete. Shared by the desktop
 * sidebar and the mobile sheet.
 */
export function PlaygroundSessionList({
  sessions,
  activeSessionId,
  disabled,
  onCreate,
  onSwitch,
  onRename,
  onDelete,
  onRevealMatches,
  onNavigate,
}: PlaygroundSessionListProps) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const [editingId, setEditingId] = useState<string | null>(null)
  const [editingTitle, setEditingTitle] = useState('')
  const [pendingDelete, setPendingDelete] = useState<Session | null>(null)

  const visibleSessions = useMemo(
    () =>
      getSortedSessions(sessions).filter((session) =>
        sessionMatchesQuery(session, query)
      ),
    [sessions, query]
  )

  const getTitle = (session: Session) =>
    session.title === DEFAULT_SESSION_TITLE ? t('New Chat') : session.title

  const startRename = (session: Session) => {
    setEditingId(session.id)
    setEditingTitle(getTitle(session))
  }

  const commitRename = () => {
    if (editingId && editingTitle.trim()) {
      onRename(editingId, editingTitle)
    }
    setEditingId(null)
  }

  const handleOpen = (session: Session) => {
    if (disabled) return
    onSwitch(session.id)
    const matches = getMatchingMessageKeys(session, query)
    if (matches.length > 0) {
      onRevealMatches(session.id, matches)
    }
    onNavigate?.()
  }

  return (
    <div className='flex min-h-0 flex-1 flex-col'>
      <div className='grid gap-2 px-3 pt-3 pb-2'>
        <Button
          className='w-full justify-start gap-2'
          disabled={disabled}
          onClick={() => {
            onCreate()
            setQuery('')
            onNavigate?.()
          }}
          size='sm'
          variant='outline'
        >
          <PlusIcon className='size-4' />
          {t('New chat')}
        </Button>
        <div className='relative'>
          <SearchIcon className='text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2' />
          <Input
            aria-label={t('Search conversations')}
            className='h-8 pr-7 pl-8 text-sm'
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('Search conversations')}
            value={query}
          />
          {query && (
            <button
              aria-label={t('Clear')}
              className='text-muted-foreground hover:text-foreground absolute top-1/2 right-2 -translate-y-1/2'
              onClick={() => setQuery('')}
              type='button'
            >
              <XIcon className='size-3.5' />
            </button>
          )}
        </div>
      </div>

      <div className='min-h-0 flex-1 overflow-y-auto px-2 pb-3'>
        {visibleSessions.length === 0 ? (
          <p className='text-muted-foreground px-2 py-6 text-center text-xs'>
            {t('No matching conversations')}
          </p>
        ) : (
          <ul className='grid gap-0.5'>
            {visibleSessions.map((session) => {
              const isActive = session.id === activeSessionId
              const matchCount = query.trim()
                ? getMatchingMessageKeys(session, query).length
                : 0

              if (editingId === session.id) {
                return (
                  <li key={session.id} className='px-1 py-0.5'>
                    <Input
                      aria-label={t('Rename')}
                      autoFocus
                      className='h-8 text-sm'
                      onBlur={commitRename}
                      onChange={(event) => setEditingTitle(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === 'Enter') commitRename()
                        if (event.key === 'Escape') setEditingId(null)
                      }}
                      value={editingTitle}
                    />
                  </li>
                )
              }

              return (
                <li key={session.id}>
                  <div
                    className={cn(
                      'group/session relative flex items-center rounded-lg transition-colors',
                      isActive
                        ? 'bg-muted text-foreground'
                        : 'text-muted-foreground hover:bg-muted/60 hover:text-foreground'
                    )}
                  >
                    <button
                      className='flex min-w-0 flex-1 flex-col items-start gap-0.5 px-2.5 py-2 text-left disabled:cursor-not-allowed'
                      disabled={disabled && !isActive}
                      onClick={() => handleOpen(session)}
                      type='button'
                    >
                      <span className='w-full truncate pr-6 text-sm font-medium'>
                        {getTitle(session)}
                      </span>
                      <span className='text-muted-foreground flex w-full items-center gap-1.5 text-[11px]'>
                        <span className='tabular-nums'>
                          {formatSessionTime(session.updatedAt)}
                        </span>
                        {matchCount > 0 ? (
                          <span className='text-primary'>
                            {t('{{count}} matches', { count: matchCount })}
                          </span>
                        ) : (
                          session.messages.length > 0 && (
                            <span>
                              {t('{{count}} messages', {
                                count: session.messages.length,
                              })}
                            </span>
                          )
                        )}
                      </span>
                    </button>

                    <DropdownMenu modal={false}>
                      <DropdownMenuTrigger asChild>
                        <Button
                          aria-label={t('More actions')}
                          className='text-muted-foreground hover:text-foreground data-[state=open]:bg-background absolute right-1 size-7 opacity-0 group-hover/session:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100 max-md:opacity-100'
                          size='icon'
                          variant='ghost'
                        >
                          <MoreHorizontalIcon className='size-4' />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align='end' className='w-44'>
                        <DropdownMenuItem onClick={() => startRename(session)}>
                          <PencilIcon className='size-4' />
                          {t('Rename')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={session.messages.length === 0}
                          onClick={() => exportSession(session, 'md')}
                        >
                          <DownloadIcon className='size-4' />
                          {t('Export as Markdown')}
                        </DropdownMenuItem>
                        <DropdownMenuItem
                          disabled={session.messages.length === 0}
                          onClick={() => exportSession(session, 'json')}
                        >
                          <DownloadIcon className='size-4' />
                          {t('Export as JSON')}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem
                          disabled={disabled}
                          onClick={() => setPendingDelete(session)}
                          variant='destructive'
                        >
                          <Trash2Icon className='size-4' />
                          {t('Delete')}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>
                </li>
              )
            })}
          </ul>
        )}
      </div>

      <ConfirmDialog
        confirmText={t('Delete')}
        desc={t(
          'The conversation "{{title}}" will be removed from this browser. This cannot be undone.',
          { title: pendingDelete ? getTitle(pendingDelete) : '' }
        )}
        destructive
        handleConfirm={() => {
          if (pendingDelete) onDelete(pendingDelete.id)
          setPendingDelete(null)
        }}
        onOpenChange={(open) => !open && setPendingDelete(null)}
        open={pendingDelete !== null}
        title={t('Delete conversation?')}
      />
    </div>
  )
}

type PlaygroundSessionSidebarProps = PlaygroundSessionListProps & {
  collapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
}

export function PlaygroundSessionSidebar({
  collapsed,
  onCollapsedChange,
  ...listProps
}: PlaygroundSessionSidebarProps) {
  const { t } = useTranslation()

  if (collapsed) {
    return (
      <aside className='border-border/60 bg-muted/20 hidden w-12 shrink-0 flex-col items-center gap-1 border-r py-3 md:flex'>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t('Show conversations')}
              className='text-muted-foreground size-8'
              onClick={() => onCollapsedChange(false)}
              size='icon'
              variant='ghost'
            >
              <PanelLeftOpenIcon className='size-4' />
            </Button>
          </TooltipTrigger>
          <TooltipContent side='right'>
            <p>{t('Show conversations')}</p>
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t('New chat')}
              className='text-muted-foreground size-8'
              disabled={listProps.disabled}
              onClick={listProps.onCreate}
              size='icon'
              variant='ghost'
            >
              <PlusIcon className='size-4' />
            </Button>
          </TooltipTrigger>
          <TooltipContent side='right'>
            <p>{t('New chat')}</p>
          </TooltipContent>
        </Tooltip>
      </aside>
    )
  }

  return (
    <aside className='border-border/60 bg-muted/20 hidden w-64 shrink-0 flex-col border-r md:flex'>
      <div className='flex items-center justify-between gap-2 px-3 pt-3'>
        <div className='text-muted-foreground flex items-center gap-2 text-xs font-medium'>
          <MessageSquareIcon className='size-3.5' />
          {t('Conversations')}
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              aria-label={t('Hide conversations')}
              className='text-muted-foreground size-7'
              onClick={() => onCollapsedChange(true)}
              size='icon'
              variant='ghost'
            >
              <PanelLeftCloseIcon className='size-4' />
            </Button>
          </TooltipTrigger>
          <TooltipContent side='right'>
            <p>{t('Hide conversations')}</p>
          </TooltipContent>
        </Tooltip>
      </div>
      <PlaygroundSessionList {...listProps} />
    </aside>
  )
}
