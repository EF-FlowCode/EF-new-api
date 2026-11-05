import { nanoid } from 'nanoid'
import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants'
import type {
  Message,
  MessageAttachment,
  MessageVersion,
  ChatCompletionMessage,
  ContentPart,
} from '../../types'

/**
 * Create a new message version
 */
export function createMessageVersion(content: string): MessageVersion {
  return {
    id: nanoid(),
    content,
  }
}

/**
 * Index of the version being shown; defaults to the newest one.
 */
export function getActiveVersionIndex(message: Message): number {
  const lastIndex = Math.max(0, message.versions.length - 1)
  const index = message.activeVersion ?? lastIndex
  return Math.min(Math.max(0, index), lastIndex)
}

/**
 * Get the version being shown (regenerated answers add versions)
 */
export function getCurrentVersion(message: Message): MessageVersion {
  return (
    message.versions[getActiveVersionIndex(message)] || {
      id: 'default',
      content: '',
    }
  )
}

/**
 * Get displayable content from the current message version.
 */
export function getMessageContent(message: Message): string {
  return getCurrentVersion(message).content
}

/**
 * Check whether a message has non-empty content in its current version.
 */
export function hasMessageContent(message: Message): boolean {
  return getMessageContent(message).trim() !== ''
}

/**
 * Attachments of a user message.
 */
export function getMessageAttachments(message: Message): MessageAttachment[] {
  return message.attachments ?? []
}

export function hasMessageAttachments(message: Message): boolean {
  return getMessageAttachments(message).length > 0
}

/**
 * Update current version content in message
 */
export function updateCurrentVersionContent(
  message: Message,
  content: string
): Message {
  const activeIndex = getActiveVersionIndex(message)
  if (message.versions.length === 0) {
    return { ...message, versions: [createMessageVersion(content)] }
  }

  return {
    ...message,
    versions: message.versions.map((version, index) =>
      index === activeIndex ? { ...version, content } : version
    ),
  }
}

/**
 * Create a user message
 */
export function createUserMessage(
  content: string,
  createdAt: number = Date.now(),
  attachments: MessageAttachment[] = []
): Message {
  const message: Message = {
    key: nanoid(),
    from: MESSAGE_ROLES.USER,
    versions: [createMessageVersion(content)],
    createdAt,
  }
  if (attachments.length > 0) {
    message.attachments = attachments
  }
  return message
}

/**
 * Create a loading assistant message
 */
export function createLoadingAssistantMessage(
  startedAt: number = Date.now()
): Message {
  return {
    key: nanoid(),
    from: MESSAGE_ROLES.ASSISTANT,
    versions: [createMessageVersion('')],
    createdAt: startedAt,
    startedAt,
    reasoning: undefined,
    isReasoningComplete: false,
    isContentComplete: false,
    isReasoningStreaming: false,
    status: MESSAGE_STATUS.LOADING,
  }
}

function attachmentToContentPart(
  attachment: MessageAttachment
): ContentPart | null {
  switch (attachment.kind) {
    case 'image':
      return attachment.url
        ? { type: 'image_url', image_url: { url: attachment.url } }
        : null
    case 'file':
      return attachment.url
        ? {
            type: 'file',
            file: { filename: attachment.name, file_data: attachment.url },
          }
        : null
    case 'text':
      return {
        type: 'text',
        text: `[File: ${attachment.name}]\n${attachment.text ?? ''}`,
      }
  }
}

/**
 * Build message content with optional attachments. Attachments whose data
 * was dropped from local history are skipped.
 */
export function buildMessageContent(
  text: string,
  attachments: MessageAttachment[] = []
): string | ContentPart[] {
  const attachmentParts = attachments
    .map(attachmentToContentPart)
    .filter((part): part is ContentPart => part !== null)

  if (attachmentParts.length === 0) {
    return text
  }

  const parts: ContentPart[] = text.trim()
    ? [{ type: 'text', text }, ...attachmentParts]
    : attachmentParts

  return parts
}

/**
 * Extract text content from message content
 */
export function getTextContent(content: string | ContentPart[]): string {
  if (typeof content === 'string') {
    return content
  }

  if (Array.isArray(content)) {
    const textPart = content.find((part) => part.type === 'text')
    return textPart?.text || ''
  }

  return ''
}

/**
 * Format message for API request
 */
export function formatMessageForAPI(message: Message): ChatCompletionMessage {
  const currentVersion = getCurrentVersion(message)
  return {
    role: message.from,
    content:
      message.from === MESSAGE_ROLES.USER
        ? buildMessageContent(
            currentVersion.content,
            getMessageAttachments(message)
          )
        : currentVersion.content,
  }
}

/**
 * Check if message is valid for API request
 * Excludes loading/streaming assistant messages and empty content
 */
export function isValidMessage(message: Message): boolean {
  if (!message || !message.from || !message.versions.length) return false

  // Exclude empty assistant messages (loading/streaming placeholders)
  if (message.from === MESSAGE_ROLES.ASSISTANT && !hasMessageContent(message)) {
    return false
  }

  return true
}
