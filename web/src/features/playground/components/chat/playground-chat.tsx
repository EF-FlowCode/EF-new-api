import { useEffect, useState } from 'react'
import { ChevronUpIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Conversation,
  ConversationContent,
  ConversationScrollButton,
} from '@/components/ai-elements/conversation'
import { Loader } from '@/components/ai-elements/loader'
import { Message } from '@/components/ai-elements/message'
import {
  getChatMessageRenderState,
  getEditingMessageContent,
  getMessageAlignment,
  getPreviousUserMessage,
  isErrorMessage,
} from '../../lib'
import type {
  Message as MessageType,
  PlaygroundMessageLayoutMode,
} from '../../types'
import { MessageActions } from '../message/message-actions'
import { MessageErrorActions } from '../message/message-error-actions'
import { MessageVersionSwitcher } from '../message/message-version-switcher'
import { PlaygroundMessageContent } from '../message/playground-message-content'
import { PlaygroundMessageEditor } from '../message/playground-message-editor'
import { PlaygroundEmptyState } from './playground-empty-state'

const MAX_RENDERED_HISTORY_MESSAGES = 24
const REVEAL_HIGHLIGHT_CLASSES = ['ring-2', 'ring-primary/50', 'rounded-xl']
const REVEAL_HIGHLIGHT_MS = 1600

export type PlaygroundChatRevealTarget = {
  messageKey: string
  // 每次定位递增，同一条消息也能重复触发滚动
  nonce: number
}

interface PlaygroundChatProps {
  messages: MessageType[]
  onCopyMessage?: (message: MessageType) => void
  onRegenerateMessage?: (message: MessageType) => void
  onEditMessage?: (message: MessageType) => void
  onDeleteMessage?: (message: MessageType) => void
  onSwitchVersion?: (message: MessageType, versionIndex: number) => void
  onSelectPrompt?: (prompt: string) => void
  isGenerating?: boolean
  isLoadingMessages?: boolean
  editingKey?: string | null
  onSaveEdit?: (newContent: string) => void
  onCancelEdit?: (open: boolean) => void
  onSaveEditAndSubmit?: (newContent: string) => void
  messageLayoutMode?: PlaygroundMessageLayoutMode
  revealTarget?: PlaygroundChatRevealTarget | null
}

export function PlaygroundChat({
  messages,
  onCopyMessage,
  onRegenerateMessage,
  onEditMessage,
  onDeleteMessage,
  onSwitchVersion,
  onSelectPrompt,
  isGenerating = false,
  isLoadingMessages = false,
  editingKey,
  onSaveEdit,
  onCancelEdit,
  onSaveEditAndSubmit,
  messageLayoutMode = 'alternating',
  revealTarget = null,
}: PlaygroundChatProps) {
  const { t } = useTranslation()
  const [editText, setEditText] = useState('')
  const [originalText, setOriginalText] = useState('')
  const [sourceMessageKeys, setSourceMessageKeys] = useState<
    ReadonlySet<string>
  >(() => new Set())
  // 默认只渲染最近 24 条；"显示更早的消息"或搜索定位会把窗口往前扩
  const [extraHistoryCount, setExtraHistoryCount] = useState(0)
  const revealIndex = revealTarget
    ? messages.findIndex((message) => message.key === revealTarget.messageKey)
    : -1
  const defaultOffset = Math.max(
    0,
    messages.length - MAX_RENDERED_HISTORY_MESSAGES - extraHistoryCount
  )
  const visibleMessageOffset =
    revealIndex >= 0 ? Math.min(defaultOffset, revealIndex) : defaultOffset
  const visibleMessages = messages.slice(visibleMessageOffset)

  useEffect(() => {
    if (!revealTarget) return
    const frame = window.requestAnimationFrame(() => {
      const element = document.querySelector<HTMLElement>(
        `[data-message-key="${CSS.escape(revealTarget.messageKey)}"]`
      )
      if (!element) return
      element.scrollIntoView({ block: 'center', behavior: 'smooth' })
      element.classList.add(...REVEAL_HIGHLIGHT_CLASSES)
      window.setTimeout(
        () => element.classList.remove(...REVEAL_HIGHLIGHT_CLASSES),
        REVEAL_HIGHLIGHT_MS
      )
    })
    return () => window.cancelAnimationFrame(frame)
  }, [revealTarget])

  function handleToggleMessageSource(message: MessageType): void {
    setSourceMessageKeys((currentKeys) => {
      const nextKeys = new Set(currentKeys)

      if (nextKeys.has(message.key)) {
        nextKeys.delete(message.key)
      } else {
        nextKeys.add(message.key)
      }

      return nextKeys
    })
  }

  useEffect(() => {
    if (!editingKey) return
    const content = getEditingMessageContent(messages, editingKey)
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setEditText(content)

    setOriginalText(content)
  }, [editingKey, messages])

  let chatContent = visibleMessages.map((message, visibleMessageIndex) => {
    const messageIndex = visibleMessageOffset + visibleMessageIndex
    const { alwaysShowActions, content, isEditing } = getChatMessageRenderState(
      messages,
      message,
      messageIndex,
      editingKey
    )
    const isError = isErrorMessage(message)
    const previousUserMessage = isError
      ? getPreviousUserMessage(messages, messageIndex)
      : null
    const alignment = getMessageAlignment(message, messageLayoutMode)
    const isSourceVisible = sourceMessageKeys.has(message.key)

    return (
      <Message
        className='group flex-row-reverse py-2.5 transition-shadow'
        data-message-key={message.key}
        from={message.from}
        key={message.key}
      >
        <div className='w-full min-w-0 flex-1 basis-full'>
          {isEditing ? (
            <PlaygroundMessageEditor
              editText={editText}
              message={message}
              onCancelEdit={onCancelEdit}
              onEditTextChange={setEditText}
              onSaveEdit={onSaveEdit}
              onSaveEditAndSubmit={onSaveEditAndSubmit}
              originalText={originalText}
            />
          ) : (
            <PlaygroundMessageContent
              alignment={alignment}
              actions={
                <MessageActions
                  message={message}
                  onCopy={onCopyMessage}
                  onRegenerate={onRegenerateMessage}
                  onToggleSource={handleToggleMessageSource}
                  onEdit={onEditMessage}
                  onDelete={onDeleteMessage}
                  isSourceVisible={isSourceVisible}
                  isGenerating={isGenerating}
                  alwaysVisible={alwaysShowActions}
                />
              }
              versionSwitcher={
                <MessageVersionSwitcher
                  disabled={isGenerating}
                  message={message}
                  onSwitch={onSwitchVersion}
                />
              }
              isSourceVisible={isSourceVisible}
              message={message}
              errorActions={
                isError ? (
                  <MessageErrorActions
                    disabled={isGenerating}
                    onRetry={
                      onRegenerateMessage
                        ? () => onRegenerateMessage(message)
                        : undefined
                    }
                    onEditPrompt={
                      onEditMessage && previousUserMessage
                        ? () => onEditMessage(previousUserMessage)
                        : undefined
                    }
                    onDelete={
                      onDeleteMessage
                        ? () => onDeleteMessage(message)
                        : undefined
                    }
                  />
                ) : undefined
              }
              versionContent={content}
            />
          )}
        </div>
      </Message>
    )
  })

  if (visibleMessages.length === 0 && onSelectPrompt) {
    chatContent = [
      <PlaygroundEmptyState key='empty' onSelectPrompt={onSelectPrompt} />,
    ]
  }

  if (isLoadingMessages) {
    chatContent = [
      <div
        className='text-muted-foreground flex min-h-[min(520px,calc(100svh-18rem))] items-center justify-center gap-2 text-sm'
        key='loading'
      >
        <Loader />
        <span>{t('Loading conversation...')}</span>
      </div>,
    ]
  }

  return (
    <Conversation>
      {/* Remove outer padding; apply padding to inner centered container to align with input */}
      <ConversationContent className='p-0'>
        <div className='mx-auto w-full max-w-4xl px-4 py-4'>
          {visibleMessageOffset > 0 && !isLoadingMessages && (
            <div className='flex justify-center pb-2'>
              <Button
                className='text-muted-foreground h-7 gap-1 rounded-full px-3 text-xs'
                onClick={() =>
                  setExtraHistoryCount(
                    (count) => count + MAX_RENDERED_HISTORY_MESSAGES
                  )
                }
                size='sm'
                variant='ghost'
              >
                <ChevronUpIcon className='size-3.5' />
                {t('Show {{count}} earlier messages', {
                  count: visibleMessageOffset,
                })}
              </Button>
            </div>
          )}
          {chatContent}
        </div>
      </ConversationContent>
      <ConversationScrollButton />
    </Conversation>
  )
}
