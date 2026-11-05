import { MESSAGE_ROLES, MESSAGE_STATUS } from '../../constants'
import type { Message } from '../../types'
import {
  getMessageContent,
  hasMessageContent,
  hasMessageAttachments,
} from './message-utils'

type MessageActionState = {
  content: string
  // 有文本或图片：可重生成 / 编辑；复制只看 hasText
  hasContent: boolean
  hasText: boolean
  isAssistant: boolean
  isLoading: boolean
  isUser: boolean
}

export function getMessageActionState(message: Message): MessageActionState {
  return {
    content: getMessageContent(message),
    hasContent: hasMessageContent(message) || hasMessageAttachments(message),
    hasText: hasMessageContent(message),
    isAssistant: message.from === MESSAGE_ROLES.ASSISTANT,
    isUser: message.from === MESSAGE_ROLES.USER,
    isLoading:
      message.status === MESSAGE_STATUS.LOADING ||
      message.status === MESSAGE_STATUS.STREAMING,
  }
}

export function getMessageActionsVisibilityClass(
  alwaysVisible: boolean
): string {
  return alwaysVisible
    ? 'opacity-100'
    : 'opacity-0 group-hover:opacity-100 max-md:opacity-100'
}
