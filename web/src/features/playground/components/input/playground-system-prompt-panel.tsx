import { useState } from 'react'
import { BookmarkPlusIcon, ScrollTextIcon, Trash2Icon } from 'lucide-react'
import { nanoid } from 'nanoid'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { useIsMobile } from '@/hooks/use-mobile'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Textarea } from '@/components/ui/textarea'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { PromptInputButton } from '@/components/ai-elements/prompt-input'
import { loadSystemPresets, saveSystemPresets } from '../../lib'
import { PLAYGROUND_PARAMETER_PANEL_SCROLL_CLASS } from '../../lib/parameters/playground-parameters'
import type { SystemPreset } from '../../types'

type PlaygroundSystemPromptPanelProps = {
  disabled?: boolean
  onChange: (value: string) => void
  value: string
}

type SystemPromptContentProps = PlaygroundSystemPromptPanelProps & {
  compact?: boolean
}

function SystemPromptContent({
  compact = false,
  disabled,
  onChange,
  value,
}: SystemPromptContentProps) {
  const { t } = useTranslation()
  const [presets, setPresets] = useState<SystemPreset[]>(loadSystemPresets)
  const [presetName, setPresetName] = useState('')

  const persistPresets = (nextPresets: SystemPreset[]) => {
    setPresets(nextPresets)
    saveSystemPresets(nextPresets)
  }

  const handleSavePreset = () => {
    const name = presetName.trim()
    if (!name || !value.trim()) return
    persistPresets([...presets, { id: nanoid(), name, content: value }])
    setPresetName('')
  }

  return (
    <div
      className={cn(
        'grid gap-3',
        PLAYGROUND_PARAMETER_PANEL_SCROLL_CLASS,
        compact ? 'px-4 pb-4' : 'p-1'
      )}
    >
      <div className='grid gap-2'>
        <Textarea
          aria-label={t('System prompt')}
          className='max-h-60 min-h-28 resize-y text-sm leading-6'
          disabled={disabled}
          onChange={(event) => onChange(event.target.value)}
          placeholder={t('Set a system prompt for this conversation…')}
          value={value}
        />
        <div className='flex items-center justify-between gap-2'>
          <span className='text-muted-foreground text-xs tabular-nums'>
            {t('{{count}} characters', { count: value.trim().length })}
          </span>
          <Button
            className='text-muted-foreground h-7 px-2 text-xs'
            disabled={disabled || !value}
            onClick={() => onChange('')}
            size='sm'
            variant='ghost'
          >
            {t('Clear')}
          </Button>
        </div>
      </div>

      <div className='border-border/70 grid gap-2 border-t pt-3'>
        <div className='text-sm font-medium'>{t('Presets')}</div>
        {presets.length === 0 ? (
          <p className='text-muted-foreground text-xs leading-4'>
            {t('Save the current prompt as a preset to reuse it later.')}
          </p>
        ) : (
          <div className='grid gap-1'>
            {presets.map((preset) => (
              <div
                className='group/preset hover:bg-muted/70 flex items-center gap-1 rounded-md pr-1'
                key={preset.id}
              >
                <button
                  className='min-w-0 flex-1 truncate px-2 py-1.5 text-left text-sm disabled:opacity-50'
                  disabled={disabled}
                  onClick={() => onChange(preset.content)}
                  title={preset.content}
                  type='button'
                >
                  {preset.name}
                </button>
                <Button
                  aria-label={t('Delete preset')}
                  className='text-muted-foreground hover:text-destructive size-7 opacity-0 group-hover/preset:opacity-100 focus-visible:opacity-100 max-md:opacity-100'
                  onClick={() =>
                    persistPresets(
                      presets.filter((item) => item.id !== preset.id)
                    )
                  }
                  size='icon'
                  variant='ghost'
                >
                  <Trash2Icon className='size-3.5' />
                </Button>
              </div>
            ))}
          </div>
        )}
        <div className='flex items-center gap-2'>
          <Input
            aria-label={t('Preset name')}
            className='h-8 text-sm'
            disabled={disabled || !value.trim()}
            onChange={(event) => setPresetName(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === 'Enter') {
                event.preventDefault()
                handleSavePreset()
              }
            }}
            placeholder={t('Preset name')}
            value={presetName}
          />
          <Button
            className='h-8 shrink-0'
            disabled={disabled || !value.trim() || !presetName.trim()}
            onClick={handleSavePreset}
            size='sm'
            variant='outline'
          >
            <BookmarkPlusIcon className='size-3.5' />
            {t('Save as preset')}
          </Button>
        </div>
      </div>
    </div>
  )
}

export function PlaygroundSystemPromptPanel(
  props: PlaygroundSystemPromptPanelProps
) {
  const { t } = useTranslation()
  const isMobile = useIsMobile()
  const hasPrompt = props.value.trim().length > 0

  const trigger = (
    <PromptInputButton
      aria-label={t('System prompt')}
      className={cn(
        'hover:bg-muted/70 relative font-medium',
        hasPrompt
          ? 'text-primary hover:text-primary'
          : 'text-muted-foreground hover:text-foreground'
      )}
      disabled={props.disabled}
      variant='ghost'
    >
      <ScrollTextIcon size={16} />
      {hasPrompt && (
        <span className='bg-primary absolute top-1 right-1 size-1.5 rounded-full' />
      )}
    </PromptInputButton>
  )

  if (isMobile) {
    return (
      <Sheet>
        <Tooltip>
          <TooltipTrigger asChild>
            <SheetTrigger asChild>{trigger}</SheetTrigger>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('System prompt')}</p>
          </TooltipContent>
        </Tooltip>
        <SheetContent
          className='max-h-[85vh] overflow-hidden rounded-t-xl'
          side='bottom'
        >
          <SheetHeader>
            <SheetTitle>{t('System prompt')}</SheetTitle>
          </SheetHeader>
          <SystemPromptContent {...props} compact />
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <Popover>
      <Tooltip>
        <TooltipTrigger asChild>
          <PopoverTrigger asChild>{trigger}</PopoverTrigger>
        </TooltipTrigger>
        <TooltipContent>
          <p>{t('System prompt')}</p>
        </TooltipContent>
      </Tooltip>
      <PopoverContent
        align='start'
        className='w-[26rem] max-w-[calc(100vw-2rem)] gap-3 p-3'
        collisionPadding={8}
        side='top'
        sideOffset={8}
      >
        <div className='space-y-1 px-1'>
          <div className='text-sm font-semibold'>{t('System prompt')}</div>
          <div className='text-muted-foreground text-xs leading-4'>
            {t(
              'Sent as the first system message of this conversation. Each conversation keeps its own prompt.'
            )}
          </div>
        </div>
        <SystemPromptContent {...props} />
      </PopoverContent>
    </Popover>
  )
}
