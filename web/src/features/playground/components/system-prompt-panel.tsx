import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { nanoid } from 'nanoid'
import { ChevronDownIcon, ChevronRightIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Textarea } from '@/components/ui/textarea'
import { loadSystemPresets, saveSystemPresets } from '../lib'
import type { SystemPreset } from '../types'

interface SystemPromptPanelProps {
  value: string
  disabled?: boolean
  onChange: (text: string) => void
}

export function SystemPromptPanel({
  value,
  disabled,
  onChange,
}: SystemPromptPanelProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [presets, setPresets] = useState<SystemPreset[]>(() =>
    loadSystemPresets()
  )
  const [selected, setSelected] = useState('')

  const persistPresets = (next: SystemPreset[]) => {
    setPresets(next)
    saveSystemPresets(next)
  }

  const handleApply = (id: string) => {
    setSelected(id)
    const p = presets.find((x) => x.id === id)
    if (p) onChange(p.content)
  }

  const handleSaveAsPreset = () => {
    if (typeof window === 'undefined') return
    const name = window.prompt(t('Preset name'))
    if (!name || !name.trim()) return
    persistPresets([
      ...presets,
      { id: nanoid(), name: name.trim(), content: value },
    ])
  }

  const handleDeletePreset = () => {
    if (!selected) return
    persistPresets(presets.filter((p) => p.id !== selected))
    setSelected('')
  }

  return (
    <div className='border-b'>
      <button
        type='button'
        className='text-muted-foreground hover:text-foreground flex w-full items-center gap-1 px-4 py-1.5 text-xs'
        onClick={() => setOpen((v) => !v)}
      >
        {open ? (
          <ChevronDownIcon className='size-3' />
        ) : (
          <ChevronRightIcon className='size-3' />
        )}
        {t('System prompt')}
        {value.trim() ? ` · ${value.trim().length}` : ''}
      </button>
      {open && (
        <div className='space-y-2 px-4 pb-2'>
          <Textarea
            value={value}
            disabled={disabled}
            onChange={(e) => onChange(e.target.value)}
            placeholder={t('Set a system prompt for this session…')}
            className='min-h-[64px] text-sm'
            rows={3}
          />
          <div className='flex flex-wrap items-center gap-2'>
            <select
              className='bg-background h-7 min-w-0 max-w-[40%] flex-1 truncate rounded-md border px-2 text-xs'
              value={selected}
              disabled={disabled || presets.length === 0}
              onChange={(e) => handleApply(e.target.value)}
              aria-label={t('Presets')}
            >
              <option value=''>{t('Presets')}…</option>
              {presets.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
            <Button
              size='sm'
              variant='ghost'
              className='h-7 px-2 text-xs'
              disabled={disabled || !value.trim()}
              onClick={handleSaveAsPreset}
            >
              {t('Save as preset')}
            </Button>
            <Button
              size='sm'
              variant='ghost'
              className='h-7 px-2 text-xs'
              disabled={disabled || !selected}
              onClick={handleDeletePreset}
            >
              {t('Delete preset')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
