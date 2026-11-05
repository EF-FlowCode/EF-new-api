import type { GroupOption, ModelOption } from '../../types'

type InputControlStateOptions = {
  disabled?: boolean
  groups: GroupOption[]
  hasAttachments?: boolean
  hasStopHandler: boolean
  isGenerating?: boolean
  isModelLoading?: boolean
  models: ModelOption[]
  text: string
}

type InputControlState = {
  canSubmit: boolean
  isSelectorDisabled: boolean
  shouldShowStop: boolean
}

type SubmittableInputFile = {
  filename?: string
  mediaType?: string
  url?: string
}

type SubmittableInputMessage = {
  text?: string | null
  files?: SubmittableInputFile[]
}

type SubmittableInput = {
  text: string
  files: SubmittableInputFile[]
}

/**
 * Text plus attached files (PromptInput has already converted them to data
 * URLs). Either one is enough to send.
 */
export function getSubmittableInput(
  message: SubmittableInputMessage,
  disabled?: boolean
): SubmittableInput | null {
  if (disabled) return null

  const text = message.text ?? ''
  const files = (message.files ?? []).filter((file) => !!file.url)

  if (!text.trim() && files.length === 0) return null
  return { text, files }
}

export function getInputControlState({
  disabled,
  groups,
  hasAttachments = false,
  hasStopHandler,
  isGenerating,
  isModelLoading,
  models,
  text,
}: InputControlStateOptions): InputControlState {
  const hasModels = models.length > 0

  return {
    canSubmit:
      !disabled && hasModels && (text.trim().length > 0 || hasAttachments),
    isSelectorDisabled: disabled || isModelLoading || groups.length === 0,
    shouldShowStop: Boolean(isGenerating && hasStopHandler),
  }
}
