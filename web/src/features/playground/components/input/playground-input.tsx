import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  PromptInput,
  PromptInputAttachment,
  PromptInputFooter,
  PromptInputTextarea,
  usePromptInputAttachments,
  type PromptInputMessage,
} from '@/components/ai-elements/prompt-input'
import { MAX_ATTACHMENT_FILE_SIZE, MAX_ATTACHMENT_FILES } from '../../constants'
import {
  ATTACHMENT_ACCEPT,
  AttachmentError,
  buildMessageAttachments,
  getSubmittableInput,
} from '../../lib'
import type {
  ModelOption,
  GroupOption,
  MessageAttachment,
  ParameterEnabled,
  PlaygroundConfig,
} from '../../types'
import { PlaygroundInputControls } from './playground-input-controls'
import { PlaygroundInputTools } from './playground-input-tools'

interface PlaygroundInputProps {
  config: PlaygroundConfig
  onSubmit: (text: string, attachments: MessageAttachment[]) => void
  onStop?: () => void
  disabled?: boolean
  isGenerating?: boolean
  models: ModelOption[]
  modelValue: string
  onModelChange: (value: string) => void
  isModelLoading?: boolean
  groups: GroupOption[]
  groupValue: string
  onGroupChange: (value: string) => void
  hasMessages?: boolean
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onClearMessages?: () => void
  onParameterEnabledChange: (
    key: keyof ParameterEnabled,
    value: boolean
  ) => void
  parameterEnabled: ParameterEnabled
  systemPrompt: string
  onSystemPromptChange: (value: string) => void
}

function PlaygroundInputAttachments() {
  const attachments = usePromptInputAttachments()
  if (attachments.files.length === 0) return null

  return (
    <div className='flex w-full flex-wrap gap-2 px-4 pt-3'>
      {attachments.files.map((file) => (
        <PromptInputAttachment data={file} key={file.id} />
      ))}
    </div>
  )
}

export function PlaygroundInput({
  config,
  onSubmit,
  onStop,
  disabled,
  isGenerating,
  models,
  modelValue,
  onModelChange,
  isModelLoading = false,
  groups,
  groupValue,
  onGroupChange,
  hasMessages = false,
  onConfigChange,
  onClearMessages,
  onParameterEnabledChange,
  parameterEnabled,
  systemPrompt,
  onSystemPromptChange,
}: PlaygroundInputProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')
  const [isPreparingFiles, setIsPreparingFiles] = useState(false)
  const isBusy = disabled || isPreparingFiles

  const handleSubmit = async (message: PromptInputMessage) => {
    const submittable = getSubmittableInput(message, isBusy)
    if (!submittable) return

    let attachments: MessageAttachment[] = []
    if (submittable.files.length > 0) {
      setIsPreparingFiles(true)
      try {
        attachments = await buildMessageAttachments(submittable.files)
      } catch (error) {
        toast.error(
          error instanceof AttachmentError
            ? `${error.filename}: ${t(error.message)}`
            : t('Failed to read file')
        )
        // Rejecting keeps the attachments in the input so the user can fix them.
        throw error
      } finally {
        setIsPreparingFiles(false)
      }
    }

    onSubmit(submittable.text, attachments)
    setText('')
  }

  const handleAttachmentError = (error: {
    code: 'max_files' | 'max_file_size' | 'accept'
  }) => {
    if (error.code === 'max_file_size') {
      toast.error(
        t('Each file must be smaller than {{size}} MB', {
          size: MAX_ATTACHMENT_FILE_SIZE / 1024 / 1024,
        })
      )
      return
    }
    if (error.code === 'max_files') {
      toast.error(
        t('You can attach up to {{count}} files', {
          count: MAX_ATTACHMENT_FILES,
        })
      )
      return
    }
    toast.error(t('Supported files: images, PDF, Word (.docx) and text files'))
  }

  return (
    <div className='grid shrink-0 gap-4 px-1 md:pb-4'>
      <PromptInput
        accept={ATTACHMENT_ACCEPT}
        className='relative'
        groupClassName='bg-background/95 dark:bg-background/80 border-border/70 shadow-[0_18px_60px_-32px_rgba(0,0,0,0.65)] ring-1 ring-foreground/5 rounded-xl overflow-hidden transition-all duration-200 focus-within:border-primary/45 focus-within:ring-primary/15 focus-within:shadow-[0_22px_70px_-34px_rgba(0,0,0,0.75)]'
        maxFileSize={MAX_ATTACHMENT_FILE_SIZE}
        maxFiles={MAX_ATTACHMENT_FILES}
        multiple
        onError={handleAttachmentError}
        onSubmit={handleSubmit}
      >
        <PlaygroundInputAttachments />
        <PromptInputTextarea
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          className='min-h-20 px-5 pt-4 pb-3 leading-7 md:min-h-24 md:text-base'
          disabled={isBusy}
          onChange={(event) => setText(event.target.value)}
          placeholder={t('Ask anything')}
          value={text}
        />

        <PromptInputFooter className='border-border/60 bg-muted/20 dark:bg-muted/10 border-t px-3 py-2.5 backdrop-blur'>
          <PlaygroundInputControls
            disabled={isBusy}
            groups={groups}
            groupValue={groupValue}
            isGenerating={isGenerating}
            isModelLoading={isModelLoading}
            models={models}
            modelValue={modelValue}
            onGroupChange={onGroupChange}
            onModelChange={onModelChange}
            onStop={onStop}
            text={text}
            tools={
              <PlaygroundInputTools
                config={config}
                disabled={isBusy}
                hasMessages={hasMessages}
                onConfigChange={onConfigChange}
                onClearMessages={onClearMessages}
                onParameterEnabledChange={onParameterEnabledChange}
                onSystemPromptChange={onSystemPromptChange}
                parameterEnabled={parameterEnabled}
                systemPrompt={systemPrompt}
              />
            }
          />
        </PromptInputFooter>
      </PromptInput>
    </div>
  )
}
