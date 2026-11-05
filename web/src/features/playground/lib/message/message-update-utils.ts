import { ERROR_MESSAGES, MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants'
import type { Message } from '../../types'
import { completeAssistantTiming } from './message-timing-utils'
import { updateCurrentVersionContent } from './message-utils'

/**
 * Update the last assistant message with an error.
 */
export function updateAssistantMessageWithError(
  messages: Message[],
  errorMessage: string,
  errorCode?: string,
  title: string = ERROR_MESSAGES.API_REQUEST_ERROR
): Message[] {
  return updateLastAssistantMessage(messages, (message) => {
    const updatedMessage = updateCurrentVersionContent(
      message,
      `${title}: ${errorMessage}`
    )

    return completeAssistantTiming({
      ...updatedMessage,
      status: MESSAGE_STATUS.ERROR,
      isReasoningStreaming: false,
      errorCode: errorCode || null,
    })
  })
}

/**
 * Update the most recent assistant message, preserving the array when absent.
 */
export function updateLastAssistantMessage(
  messages: Message[],
  updater: (message: Message) => Message
): Message[] {
  if (messages.length === 0) return messages

  const last = messages.at(-1)
  if (!last || last.from !== MESSAGE_ROLES.ASSISTANT) return messages

  const updated = [...messages]
  updated[updated.length - 1] = updater(last)
  return updated
}
