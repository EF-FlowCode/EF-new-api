import { useState } from 'react'
import { GlobeIcon, PaperclipIcon, Trash2Icon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  PromptInputButton,
  PromptInputTools,
  usePromptInputAttachments,
} from '@/components/ai-elements/prompt-input'
import { ConfirmDialog } from '@/components/confirm-dialog'
import {
  ATTACHMENT_ACTIONS,
  FILE_DIALOG_ATTACHMENT_ACTIONS,
  getAttachmentActionNotice,
  getSearchActionNotice,
} from '../../lib'
import type { ParameterEnabled, PlaygroundConfig } from '../../types'
import { PlaygroundParameterPanel } from './playground-parameter-panel'
import { PlaygroundSystemPromptPanel } from './playground-system-prompt-panel'

type PlaygroundInputToolsProps = {
  config: PlaygroundConfig
  disabled?: boolean
  hasMessages?: boolean
  onClearMessages?: () => void
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onParameterEnabledChange: (
    key: keyof ParameterEnabled,
    value: boolean
  ) => void
  parameterEnabled: ParameterEnabled
  systemPrompt: string
  onSystemPromptChange: (value: string) => void
}

export function PlaygroundInputTools({
  config,
  disabled,
  hasMessages = false,
  onClearMessages,
  onConfigChange,
  onParameterEnabledChange,
  parameterEnabled,
  systemPrompt,
  onSystemPromptChange,
}: PlaygroundInputToolsProps) {
  const { t } = useTranslation()
  const attachments = usePromptInputAttachments()
  const [clearConfirmOpen, setClearConfirmOpen] = useState(false)

  const handleFileAction = (action: string) => {
    if (FILE_DIALOG_ATTACHMENT_ACTIONS.has(action)) {
      attachments.openFileDialog()
      return
    }
    const notice = getAttachmentActionNotice(action)
    toast.info(t(notice.title), {
      description: notice.description,
    })
  }

  const handleSearchAction = () => {
    const notice = getSearchActionNotice()
    toast.info(t(notice.title))
  }

  const handleClearMessages = () => {
    onClearMessages?.()
    setClearConfirmOpen(false)
    toast.success(t('Conversation cleared'))
  }

  return (
    <>
      <PromptInputTools className='bg-background/70 border-border/60 rounded-lg border p-1 shadow-xs'>
        <Tooltip>
          <DropdownMenu>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <PromptInputButton
                  aria-label={t('Attach')}
                  className='text-muted-foreground hover:text-foreground hover:bg-muted/70 font-medium'
                  disabled={disabled}
                  variant='ghost'
                >
                  <PaperclipIcon size={16} />
                </PromptInputButton>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>
              <p>{t('Attach')}</p>
            </TooltipContent>
            <DropdownMenuContent align='start'>
              {ATTACHMENT_ACTIONS.map(({ action, icon: Icon, label }) => (
                <DropdownMenuItem
                  key={action}
                  onClick={() => handleFileAction(action)}
                >
                  <Icon className='mr-2' size={16} />
                  {t(label)}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <PromptInputButton
              aria-label={t('Search')}
              className='text-muted-foreground hover:text-foreground hover:bg-muted/70 font-medium'
              disabled={disabled}
              onClick={handleSearchAction}
              variant='ghost'
            >
              <GlobeIcon size={16} />
            </PromptInputButton>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Search')}</p>
          </TooltipContent>
        </Tooltip>

        <PlaygroundSystemPromptPanel
          disabled={disabled}
          onChange={onSystemPromptChange}
          value={systemPrompt}
        />

        <PlaygroundParameterPanel
          config={config}
          disabled={disabled}
          onConfigChange={onConfigChange}
          onParameterEnabledChange={onParameterEnabledChange}
          parameterEnabled={parameterEnabled}
        />

        <Tooltip>
          <TooltipTrigger asChild>
            <PromptInputButton
              aria-label={t('Clear chat history')}
              className='text-muted-foreground hover:text-destructive hover:bg-destructive/10 font-medium'
              disabled={disabled || !hasMessages || !onClearMessages}
              onClick={() => setClearConfirmOpen(true)}
              variant='ghost'
            >
              <Trash2Icon size={16} />
            </PromptInputButton>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Clear chat history')}</p>
          </TooltipContent>
        </Tooltip>
      </PromptInputTools>

      <ConfirmDialog
        destructive
        desc={t(
          'All playground messages saved in this browser will be removed. This cannot be undone.'
        )}
        confirmText={t('Clear')}
        handleConfirm={handleClearMessages}
        open={clearConfirmOpen}
        onOpenChange={setClearConfirmOpen}
        title={t('Clear chat history?')}
      />
    </>
  )
}
