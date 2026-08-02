import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Plus, Shield, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { cn } from '@/lib/utils'
import {
  createEmptyRiskControlHeaderRule,
  normalizeRiskControlHeaderRule,
  RISK_CONTROL_SOURCES,
  type RiskControlHeaderRule,
  type RiskControlSource,
} from '../../lib/channel-extra-rules'

type Props = {
  value: RiskControlHeaderRule[] | undefined
  onChange: (next: RiskControlHeaderRule[]) => void
}

const SOURCE_LABELS: Record<RiskControlSource, string> = {
  username: 'Username (username)',
  user_id: 'User ID (user_id)',
  user_email: 'User email (user_email)',
  user_group: 'User group (user_group)',
  using_group: 'Using group (using_group)',
  token_id: 'Token ID (token_id)',
  request_id: 'Request ID (request_id)',
  custom: 'Custom value',
}

export function RiskControlHeadersEditor({ value, onChange }: Props) {
  const { t } = useTranslation()

  const rules = useMemo<RiskControlHeaderRule[]>(
    () => (Array.isArray(value) ? value.map(normalizeRiskControlHeaderRule) : []),
    [value]
  )

  const emit = (next: RiskControlHeaderRule[]) => {
    onChange(next.map(normalizeRiskControlHeaderRule))
  }

  const updateRule = (index: number, patch: Partial<RiskControlHeaderRule>) =>
    emit(rules.map((r, i) => (i === index ? { ...r, ...patch } : r)))

  const removeRule = (index: number) =>
    emit(rules.filter((_, i) => i !== index))

  const addRule = () => emit([...rules, createEmptyRiskControlHeaderRule()])

  return (
    <div className='space-y-3'>
      <div className='flex items-start justify-between gap-3'>
        <div className='flex items-start gap-2.5'>
          <span className='bg-primary/10 text-primary mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg'>
            <Shield className='h-3.5 w-3.5' />
          </span>
          <div>
            <div className='text-sm font-semibold'>
              {t('Upstream risk control headers')}
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Pass internal user / token information to upstream as request headers, helping the upstream apply risk control, audit, or rate limiting'
              )}
            </p>
          </div>
        </div>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={addRule}
          className='shrink-0'
        >
          <Plus className='mr-1.5 h-3.5 w-3.5' />
          {t('Add field')}
        </Button>
      </div>

      {rules.length === 0 ? (
        <div className='bg-muted/40 text-muted-foreground rounded-lg border border-dashed px-4 py-6 text-center text-sm'>
          {t('No fields')}
        </div>
      ) : (
        <div className='space-y-2'>
          {rules.map((rule, index) => {
            const isCustom = rule.source === 'custom'
            return (
              <div
                key={index}
                className='bg-card space-y-3 rounded-lg border p-3'
              >
                <div className='grid gap-3 sm:grid-cols-12'>
                  <div
                    className={cn(
                      'space-y-1.5',
                      isCustom ? 'sm:col-span-4' : 'sm:col-span-6'
                    )}
                  >
                    <Label className='text-muted-foreground text-xs font-normal'>
                      {t('Header name')}
                    </Label>
                    <Input
                      value={rule.name}
                      placeholder={t('e.g. X-User-Name')}
                      onChange={(e) =>
                        updateRule(index, { name: e.target.value })
                      }
                    />
                  </div>
                  <div
                    className={cn(
                      'space-y-1.5',
                      isCustom ? 'sm:col-span-4' : 'sm:col-span-5'
                    )}
                  >
                    <Label className='text-muted-foreground text-xs font-normal'>
                      {t('Data source')}
                    </Label>
                    <Select
                      value={rule.source}
                      onValueChange={(v) =>
                        updateRule(index, {
                          source: (RISK_CONTROL_SOURCES.includes(
                            v as RiskControlSource
                          )
                            ? v
                            : 'username') as RiskControlSource,
                        })
                      }
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {RISK_CONTROL_SOURCES.map((source) => (
                          <SelectItem key={source} value={source}>
                            {t(SOURCE_LABELS[source])}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  {isCustom && (
                    <div className='space-y-1.5 sm:col-span-3'>
                      <Label className='text-muted-foreground text-xs font-normal'>
                        {t('Custom value')}
                      </Label>
                      <Input
                        value={rule.value}
                        placeholder={t(
                          'Supports placeholders like {username} {user_id}'
                        )}
                        onChange={(e) =>
                          updateRule(index, { value: e.target.value })
                        }
                      />
                    </div>
                  )}
                  <div className='flex items-end justify-end sm:col-span-1'>
                    <Button
                      type='button'
                      size='icon'
                      variant='ghost'
                      onClick={() => removeRule(index)}
                      className='text-muted-foreground hover:text-destructive'
                    >
                      <Trash2 className='h-4 w-4' />
                    </Button>
                  </div>
                </div>
                {isCustom && (
                  <p className='text-muted-foreground text-xs'>
                    {t(
                      'Placeholders: {username} {user_id} {user_email} {user_group} {using_group} {token_id} {request_id}'
                    )}
                  </p>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
