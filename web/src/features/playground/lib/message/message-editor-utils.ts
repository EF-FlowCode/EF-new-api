import { MESSAGE_ROLES } from '../../constants'
import type { Message } from '../../types'

type MessageEditorState = {
  canSave: boolean
  hasChanged: boolean
  showSaveAndSubmit: boolean
}

export function getMessageEditorState(
  message: Message,
  editText: string,
  originalText: string
): MessageEditorState {
  const hasText = editText.trim().length > 0
  const hasChanged = editText !== originalText

  return {
    canSave: hasText && hasChanged,
    hasChanged,
    showSaveAndSubmit: message.from === MESSAGE_ROLES.USER,
  }
}
