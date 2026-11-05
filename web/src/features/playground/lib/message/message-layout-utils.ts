import { MESSAGE_ROLES } from '../../constants'
import type { Message, PlaygroundMessageLayoutMode } from '../../types'

export type MessageAlignment = 'left' | 'right'

export function getMessageAlignment(
  message: Message,
  layoutMode: PlaygroundMessageLayoutMode
): MessageAlignment {
  if (layoutMode === 'left') {
    return 'left'
  }

  return message.from === MESSAGE_ROLES.USER ? 'right' : 'left'
}

export function getMessageAlignmentClass(alignment: MessageAlignment): string {
  return alignment === 'right'
    ? 'items-end text-right'
    : 'items-start text-left'
}
