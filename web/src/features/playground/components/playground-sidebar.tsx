import { useState } from 'react'
import { ChevronLeftIcon, ChevronRightIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import type {
  Message as MessageType,
  Session,
  PlaygroundConfig,
  ParameterEnabled,
} from '../types'
import { ConversationSearch } from './conversation-search'
import { ParametersPanel } from './parameters-panel'
import { SessionBar } from './session-bar'
import { SystemPromptPanel } from './system-prompt-panel'

const COLLAPSE_KEY = 'playground_sidebar_collapsed'

interface PlaygroundSidebarProps {
  sessions: Session[]
  activeId: string
  messages: MessageType[]
  systemPrompt: string
  config: PlaygroundConfig
  parameterEnabled: ParameterEnabled
  disabled?: boolean
  onSwitch: (id: string) => void
  onCreate: () => void
  onRename: (id: string, title: string) => void
  onDelete: (id: string) => void
  onSystemPromptChange: (text: string) => void
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onParameterToggle: (key: keyof ParameterEnabled, value: boolean) => void
}

export function PlaygroundSidebar({
  sessions,
  activeId,
  messages,
  systemPrompt,
  config,
  parameterEnabled,
  disabled,
  onSwitch,
  onCreate,
  onRename,
  onDelete,
  onSystemPromptChange,
  onConfigChange,
  onParameterToggle,
}: PlaygroundSidebarProps) {
  const { t } = useTranslation()
  const [collapsed, setCollapsed] = useState(() => {
    try {
      return localStorage.getItem(COLLAPSE_KEY) === '1'
    } catch {
      return false
    }
  })

  const toggle = () => {
    setCollapsed((v) => {
      const next = !v
      try {
        localStorage.setItem(COLLAPSE_KEY, next ? '1' : '0')
      } catch {
        /* ignore */
      }
      return next
    })
  }

  return (
    <aside
      className={cn(
        'bg-background flex h-full shrink-0 flex-col border-r',
        collapsed ? 'w-9' : 'w-72'
      )}
    >
      <div className='flex items-center justify-between gap-1 border-b px-2 py-1.5'>
        {!collapsed && (
          <span className='text-muted-foreground px-1 text-xs font-medium'>
            {t('Sessions')}
          </span>
        )}
        <Button
          size='sm'
          variant='ghost'
          className='size-7 shrink-0 p-0'
          onClick={toggle}
          aria-label={t('Toggle sidebar')}
        >
          {collapsed ? (
            <ChevronRightIcon className='size-4' />
          ) : (
            <ChevronLeftIcon className='size-4' />
          )}
        </Button>
      </div>
      {!collapsed && (
        <div className='min-h-0 flex-1 overflow-y-auto'>
          <SessionBar
            sessions={sessions}
            activeId={activeId}
            disabled={disabled}
            onSwitch={onSwitch}
            onCreate={onCreate}
            onRename={onRename}
            onDelete={onDelete}
          />
          <SystemPromptPanel
            value={systemPrompt}
            disabled={disabled}
            onChange={onSystemPromptChange}
          />
          <ParametersPanel
            config={config}
            parameterEnabled={parameterEnabled}
            disabled={disabled}
            onConfigChange={onConfigChange}
            onParameterToggle={onParameterToggle}
          />
          <ConversationSearch messages={messages} />
        </div>
      )}
    </aside>
  )
}
