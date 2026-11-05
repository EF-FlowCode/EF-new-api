import { MESSAGE_ROLES } from '../../constants'
import type { Message, MessageAttachment } from '../../types'
import {
  createLoadingAssistantMessage,
  createUserMessage,
  getMessageContent,
  updateCurrentVersionContent,
} from './message-utils'
import {
  appendRegeneratedVersion,
  switchMessageVersion,
} from './message-version-utils'

type ApplyMessageEditResult = {
  messages: Message[]
  shouldSend: boolean
}

type ChatMessageRenderState = {
  alwaysShowActions: boolean
  content: string
  isEditing: boolean
}

export function appendUserMessagePair(
  messages: Message[],
  content: string,
  attachments: MessageAttachment[] = []
): Message[] {
  const submittedAt = Date.now()

  return [
    ...messages,
    createUserMessage(content, submittedAt, attachments),
    createLoadingAssistantMessage(submittedAt),
  ]
}

/**
 * Regenerate an answer. The new answer is added as another version of the
 * assistant message so earlier answers stay available; later messages are
 * dropped because they were based on the old answer.
 */
export function createRegeneratedMessages(
  messages: Message[],
  messageKey: string
): Message[] | null {
  const messageIndex = messages.findIndex(
    (message) => message.key === messageKey
  )

  if (messageIndex === -1) {
    return null
  }

  const message = messages[messageIndex]

  if (message.from === MESSAGE_ROLES.USER) {
    const answer = messages[messageIndex + 1]
    return [
      ...messages.slice(0, messageIndex + 1),
      answer?.from === MESSAGE_ROLES.ASSISTANT
        ? appendRegeneratedVersion(answer)
        : createLoadingAssistantMessage(),
    ]
  }

  if (message.from === MESSAGE_ROLES.ASSISTANT) {
    return [
      ...messages.slice(0, messageIndex),
      appendRegeneratedVersion(message),
    ]
  }

  return [...messages.slice(0, messageIndex), createLoadingAssistantMessage()]
}

export function switchMessageVersionByKey(
  messages: Message[],
  messageKey: string,
  versionIndex: number
): Message[] {
  return messages.map((message) =>
    message.key === messageKey
      ? switchMessageVersion(message, versionIndex)
      : message
  )
}

export function removeMessageByKey(
  messages: Message[],
  messageKey: string
): Message[] {
  return messages.filter((message) => message.key !== messageKey)
}

export function getPreviousUserMessage(
  messages: Message[],
  beforeIndex: number
): Message | null {
  for (let index = beforeIndex - 1; index >= 0; index--) {
    if (messages[index].from === MESSAGE_ROLES.USER) {
      return messages[index]
    }
  }

  return null
}

export function applyMessageEdit(
  messages: Message[],
  messageKey: string,
  content: string,
  shouldSubmit: boolean
): ApplyMessageEditResult | null {
  const submittedAt = Date.now()
  const messageIndex = messages.findIndex(
    (message) => message.key === messageKey
  )

  if (messageIndex === -1) {
    return null
  }

  const updatedMessages = messages.map((message) =>
    message.key === messageKey
      ? {
          ...updateCurrentVersionContent(message, content),
          createdAt: shouldSubmit ? submittedAt : message.createdAt,
        }
      : message
  )

  if (
    !shouldSubmit ||
    updatedMessages[messageIndex].from !== MESSAGE_ROLES.USER
  ) {
    return { messages: updatedMessages, shouldSend: false }
  }

  return {
    messages: [
      ...updatedMessages.slice(0, messageIndex + 1),
      createLoadingAssistantMessage(submittedAt),
    ],
    shouldSend: true,
  }
}

export function getEditingMessageContent(
  messages: Message[],
  editingKey?: string | null
): string {
  if (!editingKey) {
    return ''
  }

  const message = messages.find((item) => item.key === editingKey)
  return message ? getMessageContent(message) : ''
}

export function getChatMessageRenderState(
  messages: Message[],
  message: Message,
  messageIndex: number,
  editingKey?: string | null
): ChatMessageRenderState {
  return {
    alwaysShowActions:
      messageIndex === messages.length - 1 &&
      message.from === MESSAGE_ROLES.ASSISTANT,
    content: getMessageContent(message),
    isEditing: editingKey === message.key,
  }
}
