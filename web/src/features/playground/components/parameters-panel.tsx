import { useState } from 'react'
import { ChevronDownIcon, ChevronRightIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import type { PlaygroundConfig, ParameterEnabled } from '../types'

interface ParametersPanelProps {
  config: PlaygroundConfig
  parameterEnabled: ParameterEnabled
  disabled?: boolean
  onConfigChange: <K extends keyof PlaygroundConfig>(
    key: K,
    value: PlaygroundConfig[K]
  ) => void
  onParameterToggle: (key: keyof ParameterEnabled, value: boolean) => void
}

type ParamKey = keyof ParameterEnabled

interface ParamSpec {
  key: ParamKey
  label: string
  min?: number
  max?: number
  step: number
  /** seed has no default and may be null */
  nullable?: boolean
}

const PARAM_SPECS: ParamSpec[] = [
  { key: 'temperature', label: 'Temperature', min: 0, max: 2, step: 0.1 },
  { key: 'top_p', label: 'Top P', min: 0, max: 1, step: 0.05 },
  { key: 'max_tokens', label: 'Max tokens', min: 1, step: 1 },
  {
    key: 'frequency_penalty',
    label: 'Frequency penalty',
    min: -2,
    max: 2,
    step: 0.1,
  },
  {
    key: 'presence_penalty',
    label: 'Presence penalty',
    min: -2,
    max: 2,
    step: 0.1,
  },
  { key: 'seed', label: 'Seed', step: 1, nullable: true },
]

export function ParametersPanel({
  config,
  parameterEnabled,
  disabled,
  onConfigChange,
  onParameterToggle,
}: ParametersPanelProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const enabledCount = PARAM_SPECS.filter((s) => parameterEnabled[s.key]).length

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
        {t('Parameters')}
        {enabledCount > 0 ? ` · ${enabledCount}` : ''}
      </button>
      {open && (
        <div className='space-y-2 px-4 pb-2'>
          {PARAM_SPECS.map((spec) => {
            const isOn = parameterEnabled[spec.key]
            const raw = config[spec.key as keyof PlaygroundConfig]
            const value = raw === null || raw === undefined ? '' : String(raw)
            return (
              <div
                key={spec.key}
                className='flex items-center justify-between gap-2'
              >
                <label className='flex min-w-0 flex-1 items-center gap-2'>
                  <Switch
                    checked={isOn}
                    disabled={disabled}
                    onCheckedChange={(v) => onParameterToggle(spec.key, v)}
                    aria-label={t(spec.label)}
                  />
                  <span className='text-foreground truncate text-xs'>
                    {t(spec.label)}
                  </span>
                </label>
                <Input
                  type='number'
                  inputMode='decimal'
                  min={spec.min}
                  max={spec.max}
                  step={spec.step}
                  value={value}
                  disabled={disabled || !isOn}
                  onChange={(e) => {
                    const txt = e.target.value
                    if (txt === '') {
                      onConfigChange(
                        spec.key as keyof PlaygroundConfig,
                        (spec.nullable
                          ? null
                          : 0) as PlaygroundConfig[keyof PlaygroundConfig]
                      )
                      return
                    }
                    const num = Number(txt)
                    if (Number.isNaN(num)) return
                    onConfigChange(
                      spec.key as keyof PlaygroundConfig,
                      num as PlaygroundConfig[keyof PlaygroundConfig]
                    )
                  }}
                  className='h-7 w-20 px-2 text-right text-xs'
                />
              </div>
            )
          })}
          <p className='text-muted-foreground pt-1 text-[11px] leading-snug'>
            {t(
              'Disabled parameters are not sent. Some models (e.g. Opus 4.7) ignore temperature/top_p.'
            )}
          </p>
        </div>
      )}
    </div>
  )
}
