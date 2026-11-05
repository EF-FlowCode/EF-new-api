import { useCallback, useRef, useState } from 'react'
import { PanelLeftOpenIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  PlaygroundChat,
  type PlaygroundChatRevealTarget,
} from './components/chat/playground-chat'
import {
  PlaygroundSessionList,
  PlaygroundSessionSidebar,
} from './components/chat/playground-session-sidebar'
import { PlaygroundContextUsage } from './components/input/playground-context-usage'
import { PlaygroundInput } from './components/input/playground-input'
import {
  useChatHandler,
  usePlaygroundConversation,
  usePlaygroundOptions,
  usePlaygroundState,
} from './hooks'

const SIDEBAR_COLLAPSED_KEY = 'playground_sidebar_collapsed'

function readSidebarCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

export function Playground() {
  const { t } = useTranslation()
  const {
    config,
    parameterEnabled,
    messages,
    isLoadingMessages,
    models,
    groups,
    sessions,
    activeSessionId,
    systemPrompt,
    updateMessages,
    setModels,
    setGroups,
    updateConfig,
    updateParameterEnabled,
    clearMessages,
    createSession,
    switchSession,
    deleteSession,
    renameSession,
    setSystemPrompt,
  } = usePlaygroundState()

  const { sendChat, stopGeneration, isGenerating } = useChatHandler({
    config,
    parameterEnabled,
    systemPrompt,
    onMessageUpdate: updateMessages,
  })

  const {
    editingMessageKey,
    handleSendMessage,
    handleRegenerateMessage,
    handleEditMessage,
    handleEditOpenChange,
    applyEdit,
    handleDeleteMessage,
    handleSwitchVersion,
  } = usePlaygroundConversation({
    messages,
    updateMessages,
    sendChat,
  })

  const { isLoadingModels } = usePlaygroundOptions({
    currentGroup: config.group,
    currentModel: config.model,
    setGroups,
    setModels,
    updateConfig,
  })

  const [sidebarCollapsed, setSidebarCollapsed] = useState(readSidebarCollapsed)
  const [mobileSessionsOpen, setMobileSessionsOpen] = useState(false)
  const [revealTarget, setRevealTarget] =
    useState<PlaygroundChatRevealTarget | null>(null)
  const revealCursorRef = useRef<{ sessionId: string; index: number } | null>(
    null
  )

  const handleSidebarCollapsedChange = (collapsed: boolean) => {
    setSidebarCollapsed(collapsed)
    try {
      localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? '1' : '0')
    } catch {
      /* ignore */
    }
  }

  const handleClearMessages = () => {
    handleEditOpenChange(false)
    clearMessages()
  }

  const handleCreateSession = () => {
    handleEditOpenChange(false)
    revealCursorRef.current = null
    setRevealTarget(null)
    createSession()
  }

  const handleSwitchSession = (id: string) => {
    if (id === activeSessionId) return
    handleEditOpenChange(false)
    revealCursorRef.current = null
    setRevealTarget(null)
    switchSession(id)
  }

  // Opening the same search result again moves on to the next match.
  const handleRevealMatches = useCallback(
    (sessionId: string, messageKeys: string[]) => {
      const cursor = revealCursorRef.current
      const index =
        cursor?.sessionId === sessionId
          ? (cursor.index + 1) % messageKeys.length
          : 0
      revealCursorRef.current = { sessionId, index }
      setRevealTarget((previous) => ({
        messageKey: messageKeys[index],
        nonce: (previous?.nonce ?? 0) + 1,
      }))
    },
    []
  )

  const sessionListProps = {
    sessions,
    activeSessionId,
    disabled: isGenerating,
    onCreate: handleCreateSession,
    onSwitch: handleSwitchSession,
    onRename: renameSession,
    onDelete: deleteSession,
    onRevealMatches: handleRevealMatches,
  }

  return (
    <div className='relative flex size-full min-h-0 overflow-hidden'>
      <PlaygroundSessionSidebar
        {...sessionListProps}
        collapsed={sidebarCollapsed}
        onCollapsedChange={handleSidebarCollapsedChange}
      />

      <Sheet onOpenChange={setMobileSessionsOpen} open={mobileSessionsOpen}>
        <SheetContent className='flex w-72 flex-col gap-0 p-0' side='left'>
          <SheetHeader className='border-border/60 border-b'>
            <SheetTitle>{t('Conversations')}</SheetTitle>
          </SheetHeader>
          <PlaygroundSessionList
            {...sessionListProps}
            onNavigate={() => setMobileSessionsOpen(false)}
          />
        </SheetContent>
      </Sheet>

      <div className='relative flex min-w-0 flex-1 flex-col overflow-hidden'>
        <Button
          aria-label={t('Show conversations')}
          className='bg-background/80 absolute top-2 left-2 z-10 size-8 backdrop-blur md:hidden'
          onClick={() => setMobileSessionsOpen(true)}
          size='icon'
          variant='outline'
        >
          <PanelLeftOpenIcon className='size-4' />
        </Button>

        {/* Full-width scroll container: scrolling works even over side whitespace */}
        <div className='flex min-h-0 flex-1 flex-col overflow-hidden'>
          <PlaygroundChat
            key={activeSessionId}
            messages={messages}
            isLoadingMessages={isLoadingMessages}
            onRegenerateMessage={handleRegenerateMessage}
            onEditMessage={handleEditMessage}
            onDeleteMessage={handleDeleteMessage}
            onSwitchVersion={handleSwitchVersion}
            onSelectPrompt={handleSendMessage}
            isGenerating={isGenerating}
            editingKey={editingMessageKey}
            onCancelEdit={handleEditOpenChange}
            onSaveEdit={(newContent) => applyEdit(newContent, false)}
            onSaveEditAndSubmit={(newContent) => applyEdit(newContent, true)}
            revealTarget={revealTarget}
          />
        </div>

        {/* Input area: center content and constrain to the same container width */}
        <div className='mx-auto w-full max-w-4xl'>
          <PlaygroundInput
            config={config}
            disabled={isGenerating}
            groups={groups}
            groupValue={config.group}
            isGenerating={isGenerating}
            isModelLoading={isLoadingModels}
            modelValue={config.model}
            models={models}
            onGroupChange={(value) => updateConfig('group', value)}
            onConfigChange={updateConfig}
            onClearMessages={handleClearMessages}
            onModelChange={(value) => updateConfig('model', value)}
            onParameterEnabledChange={updateParameterEnabled}
            onStop={stopGeneration}
            onSubmit={handleSendMessage}
            onSystemPromptChange={setSystemPrompt}
            parameterEnabled={parameterEnabled}
            systemPrompt={systemPrompt}
            hasMessages={messages.length > 0}
          />
          <PlaygroundContextUsage
            disabled={isGenerating}
            messages={messages}
            onTrim={updateMessages}
            systemPrompt={systemPrompt}
          />
        </div>
      </div>
    </div>
  )
}
