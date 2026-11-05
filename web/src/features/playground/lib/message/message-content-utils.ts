import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants'
import type { Message, MessageAttachment } from '../../types'
import { parseThinkTags } from './message-reasoning-utils'
import { getMessageAttachments } from './message-utils'

type MessageContentStateBase = {
  displayContent: string
  attachments: MessageAttachment[]
  hasSources: boolean
  isAssistant: boolean
  showLoader: boolean
  showMessageContent: boolean
  sources: NonNullable<Message['sources']>
}

type MessageContentState = MessageContentStateBase &
  (
    | {
        hasReasoning: true
        reasoningContent: string
      }
    | {
        hasReasoning: false
        reasoningContent: undefined
      }
  )

function shouldShowMessageLoader(
  message: Message,
  isAssistant: boolean,
  versionContent: string
): boolean {
  return (
    isAssistant &&
    !message.isReasoningStreaming &&
    (message.status === MESSAGE_STATUS.LOADING ||
      (message.status === MESSAGE_STATUS.STREAMING && !versionContent))
  )
}

function shouldShowMessageContent(
  message: Message,
  versionContent: string
): boolean {
  return (
    (message.from === MESSAGE_ROLES.USER || !message.isReasoningStreaming) &&
    versionContent.length > 0
  )
}

function getDisplayContent(message: Message, versionContent: string): string {
  if (message.from !== MESSAGE_ROLES.ASSISTANT) {
    return versionContent
  }

  if (!versionContent.includes('<think>')) {
    return versionContent
  }

  return parseThinkTags(versionContent).visibleContent
}

export function getMessageContentState(
  message: Message,
  versionContent: string
): MessageContentState {
  const isAssistant = message.from === MESSAGE_ROLES.ASSISTANT
  const sources = message.sources ?? []
  const reasoningContent = isAssistant ? message.reasoning?.content : undefined
  const showLoader = shouldShowMessageLoader(
    message,
    isAssistant,
    versionContent
  )
  const attachments = getMessageAttachments(message)
  const showMessageContent =
    shouldShowMessageContent(message, versionContent) || attachments.length > 0

  const baseState: MessageContentStateBase = {
    displayContent: getDisplayContent(message, versionContent),
    attachments,
    hasSources: sources.length > 0,
    isAssistant,
    showLoader,
    showMessageContent,
    sources,
  }

  if (reasoningContent) {
    return {
      ...baseState,
      hasReasoning: true,
      reasoningContent,
    }
  }

  return {
    ...baseState,
    hasReasoning: false,
    reasoningContent: undefined,
  }
}
