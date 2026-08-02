import { useState } from 'react'
import {
  PaperclipIcon,
  FileIcon,
  ImageIcon,
  ScreenShareIcon,
  CameraIcon,
  GlobeIcon,
  SendIcon,
  SquareIcon,
  BarChartIcon,
  BoxIcon,
  NotepadTextIcon,
  CodeSquareIcon,
  GraduationCapIcon,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  PromptInput,
  PromptInputAttachment,
  PromptInputAttachments,
  PromptInputButton,
  PromptInputFooter,
  PromptInputTextarea,
  PromptInputTools,
  usePromptInputAttachments,
  type PromptInputMessage,
} from '@/components/ai-elements/prompt-input'
import { Suggestion, Suggestions } from '@/components/ai-elements/suggestion'
import { ModelGroupSelector } from '@/components/model-group-selector'
import type { ModelOption, GroupOption } from '../types'

interface PlaygroundInputProps {
  onSubmit: (text: string, imageDataUrls: string[]) => void
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
}

/**
 * Send 按钮：放进 <PromptInput> 内部读 attachments 数量，
 * 这样"只发图不带文字"时按钮也能亮。
 */
function SendButton({ disabled, text }: { disabled?: boolean; text: string }) {
  const { t } = useTranslation()
  const attachments = usePromptInputAttachments()
  const hasPayload = text.trim().length > 0 || attachments.files.length > 0
  return (
    <PromptInputButton
      className='text-foreground rounded-full font-medium'
      disabled={disabled || !hasPayload}
      type='submit'
      variant='secondary'
    >
      <SendIcon size={16} />
      <span className='hidden sm:inline'>{t('Send')}</span>
      <span className='sr-only sm:hidden'>{t('Send')}</span>
    </PromptInputButton>
  )
}

/**
 * Attach 下拉里 'Upload photo' / 'Upload file' 的 onClick——必须在
 * <PromptInput> 内部（usePromptInputAttachments 需要 context）。
 */
function AttachMenuItems({ disabled }: { disabled?: boolean }) {
  const { t } = useTranslation()
  const attachments = usePromptInputAttachments()
  return (
    <>
      <DropdownMenuItem
        disabled={disabled}
        onClick={() => attachments.openFileDialog()}
      >
        <ImageIcon className='mr-2' size={16} />
        {t('Upload photo')}
      </DropdownMenuItem>
      <DropdownMenuItem
        disabled={disabled}
        onClick={() => attachments.openFileDialog()}
      >
        <FileIcon className='mr-2' size={16} />
        {t('Upload file')}
      </DropdownMenuItem>
      <DropdownMenuItem
        onClick={() =>
          toast.info(t('Feature in development'), {
            description: 'take-screenshot',
          })
        }
      >
        <ScreenShareIcon className='mr-2' size={16} />
        {t('Take screenshot')}
      </DropdownMenuItem>
      <DropdownMenuItem
        onClick={() =>
          toast.info(t('Feature in development'), {
            description: 'take-photo',
          })
        }
      >
        <CameraIcon className='mr-2' size={16} />
        {t('Take photo')}
      </DropdownMenuItem>
    </>
  )
}

const suggestions = [
  { icon: BarChartIcon, text: 'Analyze data', color: '#76d0eb' },
  { icon: BoxIcon, text: 'Surprise me', color: '#76d0eb' },
  { icon: NotepadTextIcon, text: 'Summarize text', color: '#ea8444' },
  { icon: CodeSquareIcon, text: 'Code', color: '#6c71ff' },
  { icon: GraduationCapIcon, text: 'Get advice', color: '#76d0eb' },
  { icon: null, text: 'More' },
]

export function PlaygroundInput({
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
}: PlaygroundInputProps) {
  const { t } = useTranslation()
  const [text, setText] = useState('')

  const isModelSelectDisabled =
    disabled || isModelLoading || models.length === 0
  const isGroupSelectDisabled = disabled || groups.length === 0

  const handleSubmit = (message: PromptInputMessage) => {
    const text = message.text ?? ''
    // PromptInput 已经把 blob URL 转成 data URL，files[i].url 直接就是 base64 dataURL。
    const imageDataUrls = (message.files ?? [])
      .filter(
        (f) =>
          (f.mediaType?.startsWith('image/') || /^data:image\//.test(f.url)) &&
          !!f.url
      )
      .map((f) => f.url)

    if (disabled) return
    // 允许"只发图片不带文字"，所以放宽：text 与 imageDataUrls 至少一项非空
    if (!text.trim() && imageDataUrls.length === 0) return

    onSubmit(text, imageDataUrls)
    setText('')
  }

  const handleSuggestionClick = (suggestion: string) => {
    onSubmit(suggestion, [])
  }

  return (
    <div className='grid shrink-0 gap-4 px-1 md:pb-4'>
      <PromptInput
        accept='image/*'
        groupClassName='rounded-[20px] [--radius:20px]'
        maxFileSize={20 * 1024 * 1024}
        multiple
        onError={(err) => toast.error(err.message)}
        onSubmit={handleSubmit}
      >
        <PromptInputAttachments>
          {(att) => <PromptInputAttachment data={att} />}
        </PromptInputAttachments>
        <PromptInputTextarea
          autoComplete='off'
          autoCorrect='off'
          autoCapitalize='off'
          spellCheck={false}
          className='px-5 md:text-base'
          disabled={disabled}
          onChange={(event) => setText(event.target.value)}
          placeholder={t('Ask anything')}
          value={text}
        />

        <PromptInputFooter className='p-2.5'>
          <PromptInputTools>
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <PromptInputButton
                  className='!rounded-full border font-medium'
                  disabled={disabled}
                  variant='outline'
                >
                  <PaperclipIcon size={16} />
                  <span className='hidden sm:inline'>{t('Attach')}</span>
                  <span className='sr-only sm:hidden'>{t('Attach')}</span>
                </PromptInputButton>
              </DropdownMenuTrigger>
              <DropdownMenuContent align='start'>
                <AttachMenuItems disabled={disabled} />
              </DropdownMenuContent>
            </DropdownMenu>

            <PromptInputButton
              className='rounded-full border font-medium'
              disabled={disabled}
              onClick={() => toast.info(t('Search feature in development'))}
              variant='outline'
            >
              <GlobeIcon size={16} />
              <span className='hidden sm:inline'>{t('Search')}</span>
              <span className='sr-only sm:hidden'>{t('Search')}</span>
            </PromptInputButton>
          </PromptInputTools>

          <div className='flex items-center gap-1.5 md:gap-2'>
            <ModelGroupSelector
              selectedModel={modelValue}
              models={models}
              onModelChange={onModelChange}
              selectedGroup={groupValue}
              groups={groups}
              onGroupChange={onGroupChange}
              disabled={isModelSelectDisabled || isGroupSelectDisabled}
            />

            {isGenerating && onStop ? (
              <PromptInputButton
                className='text-foreground rounded-full font-medium'
                onClick={onStop}
                variant='secondary'
              >
                <SquareIcon className='fill-current' size={16} />
                <span className='hidden sm:inline'>{t('Stop')}</span>
                <span className='sr-only sm:hidden'>{t('Stop')}</span>
              </PromptInputButton>
            ) : (
              <SendButton disabled={disabled} text={text} />
            )}
          </div>
        </PromptInputFooter>
      </PromptInput>

      <Suggestions>
        {suggestions.map(({ icon: Icon, text, color }) => {
          const label = t(text)
          return (
            <Suggestion
              className={`text-xs font-normal sm:text-sm ${
                text === 'More' ? 'hidden sm:flex' : ''
              }`}
              key={text}
              onClick={() => handleSuggestionClick(label)}
              suggestion={label}
            >
              {Icon && <Icon size={16} style={{ color }} />}
              {label}
            </Suggestion>
          )
        })}
      </Suggestions>
    </div>
  )
}
