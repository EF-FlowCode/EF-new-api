import { useEffect, useMemo, useState } from 'react'
import { Plus, Save, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { MultiSelect } from '@/components/multi-select'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOptions } from '../hooks/use-update-option'

type RegionRule = {
  region: string
  display_name: string
  allowed_groups: string[]
  popup_enabled: boolean
  popup_title: string
  popup_content: string
  popup_notice: string
  popup_button: string
  version: string
}

type WebGroupAccessProps = {
  defaultValues: {
    enabled: boolean
    rules: string
  }
}

function parseRules(value: string): RegionRule[] {
  if (!value.trim()) return []
  try {
    const parsed = JSON.parse(value) as Record<string, Partial<RegionRule>>
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
      return []
    return Object.entries(parsed).map(([region, rule]) => ({
      region,
      display_name: rule.display_name || '',
      allowed_groups: Array.isArray(rule.allowed_groups)
        ? rule.allowed_groups.map(String)
        : [],
      popup_enabled: rule.popup_enabled !== false,
      popup_title: rule.popup_title || '',
      popup_content: rule.popup_content || '',
      popup_notice: rule.popup_notice || '',
      popup_button: rule.popup_button || '',
      version: rule.version || 'v1',
    }))
  } catch {
    return []
  }
}

function serializeRules(rules: RegionRule[]): string {
  const result: Record<string, Omit<RegionRule, 'region'>> = {}
  for (const rule of rules) {
    const region = rule.region.trim().toUpperCase()
    if (!region) continue
    result[region] = {
      display_name: rule.display_name.trim(),
      allowed_groups: rule.allowed_groups
        .map((group) => group.trim())
        .filter(Boolean),
      popup_enabled: rule.popup_enabled,
      popup_title: rule.popup_title.trim(),
      popup_content: rule.popup_content.trim(),
      popup_notice: rule.popup_notice.trim(),
      popup_button: rule.popup_button.trim(),
      version: rule.version.trim() || 'v1',
    }
  }
  return JSON.stringify(result)
}

const emptyRule = (): RegionRule => ({
  region: '',
  display_name: '',
  allowed_groups: [],
  popup_enabled: true,
  popup_title: '',
  popup_content: '',
  popup_notice: '',
  popup_button: '',
  version: 'v1',
})

export function WebGroupAccessSection({ defaultValues }: WebGroupAccessProps) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()
  const [enabled, setEnabled] = useState(defaultValues.enabled)
  const [rules, setRules] = useState(() => parseRules(defaultValues.rules))
  const [initialValue, setInitialValue] = useState(() =>
    JSON.stringify({
      enabled: defaultValues.enabled,
      rules: serializeRules(parseRules(defaultValues.rules)),
    })
  )
  const [availableGroups, setAvailableGroups] = useState<string[]>([])

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    const parsed = parseRules(defaultValues.rules)
    setEnabled(defaultValues.enabled)
    setRules(parsed)
    setInitialValue(
      JSON.stringify({
        enabled: defaultValues.enabled,
        rules: serializeRules(parsed),
      })
    )
  }, [defaultValues])
  /* eslint-enable react-hooks/set-state-in-effect */

  useEffect(() => {
    let active = true
    api
      .get('/api/group/')
      .then((res) => {
        if (!active || !res.data?.success) return
        const data = res.data.data
        const groups = (
          Array.isArray(data) ? data : Object.keys(data || {})
        ).filter((group: string) => group !== 'auto')
        setAvailableGroups(groups)
      })
      .catch(() => undefined)
    return () => {
      active = false
    }
  }, [])

  const currentValue = useMemo(
    () => JSON.stringify({ enabled, rules: serializeRules(rules) }),
    [enabled, rules]
  )

  const groupOptions = useMemo(
    () => availableGroups.map((group) => ({ label: group, value: group })),
    [availableGroups]
  )

  const updateRule = (index: number, patch: Partial<RegionRule>) => {
    setRules((previous) =>
      previous.map((rule, ruleIndex) =>
        ruleIndex === index ? { ...rule, ...patch } : rule
      )
    )
  }

  const handleSave = async () => {
    if (currentValue === initialValue) {
      toast.info(t('No changes to save'))
      return
    }
    await updateOptions.mutateAsync([
      { key: 'web_group_setting.enabled', value: enabled },
      { key: 'web_group_setting.rules', value: serializeRules(rules) },
    ])
    setInitialValue(currentValue)
  }

  return (
    <SettingsSection
      title={t('Web Group Region Access')}
      description={t(
        'Limit group choices in the web interface by Cloudflare country code. API requests are not affected.'
      )}
    >
      <div className='flex items-center justify-between rounded-lg border p-4'>
        <div>
          <p className='font-medium'>
            {t('Enable region-based group choices')}
          </p>
          <p className='text-muted-foreground text-sm'>
            {t('Use the CF-IPCountry header supplied by Cloudflare.')}
          </p>
        </div>
        <Switch checked={enabled} onCheckedChange={setEnabled} />
      </div>

      <div className='space-y-4'>
        {rules.map((rule, index) => (
          <div
            key={`${rule.region}-${index}`}
            className='space-y-4 rounded-lg border p-4'
          >
            <div className='flex items-center justify-between gap-3'>
              <h4 className='font-medium'>
                {rule.region || t('New region rule')}
              </h4>
              <Button
                type='button'
                variant='ghost'
                size='icon'
                onClick={() =>
                  setRules((previous) => previous.filter((_, i) => i !== index))
                }
                aria-label={t('Remove region rule')}
                title={t('Remove region rule')}
              >
                <Trash2 className='size-4' />
              </Button>
            </div>

            <div className='grid gap-4 sm:grid-cols-2'>
              <label className='space-y-2'>
                <span className='text-sm font-medium'>{t('Country code')}</span>
                <Input
                  value={rule.region}
                  onChange={(event) =>
                    updateRule(index, { region: event.target.value })
                  }
                  placeholder='CN'
                  maxLength={8}
                />
              </label>
              <label className='space-y-2'>
                <span className='text-sm font-medium'>
                  {t('Region display name')}
                </span>
                <Input
                  value={rule.display_name}
                  onChange={(event) =>
                    updateRule(index, { display_name: event.target.value })
                  }
                  placeholder={t('e.g. Mainland China')}
                />
              </label>
            </div>

            <label className='space-y-2'>
              <span className='text-sm font-medium'>{t('Allowed groups')}</span>
              <MultiSelect
                options={groupOptions}
                selected={rule.allowed_groups}
                onChange={(groups) =>
                  updateRule(index, { allowed_groups: groups })
                }
                placeholder={t('Select allowed groups')}
              />
              {availableGroups.length > 0 && (
                <span className='text-muted-foreground text-xs'>
                  {t('Configured groups')}: {availableGroups.join(', ')}
                </span>
              )}
            </label>

            <div className='flex items-center justify-between rounded-md border p-3'>
              <div>
                <p className='text-sm font-medium'>{t('Show region notice')}</p>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Show the notice every time a visitor enters the website.'
                  )}
                </p>
              </div>
              <Switch
                checked={rule.popup_enabled}
                onCheckedChange={(value) =>
                  updateRule(index, { popup_enabled: value })
                }
              />
            </div>

            {rule.popup_enabled && (
              <div className='bg-muted/30 space-y-4 rounded-md p-3'>
                <div className='grid gap-4 sm:grid-cols-2'>
                  <label className='space-y-2'>
                    <span className='text-sm font-medium'>
                      {t('Popup title')}
                    </span>
                    <Input
                      value={rule.popup_title}
                      onChange={(event) =>
                        updateRule(index, { popup_title: event.target.value })
                      }
                      placeholder={t('Need regional access permission')}
                    />
                  </label>
                  <label className='space-y-2'>
                    <span className='text-sm font-medium'>
                      {t('Confirm button text')}
                    </span>
                    <Input
                      value={rule.popup_button}
                      onChange={(event) =>
                        updateRule(index, { popup_button: event.target.value })
                      }
                      placeholder={t('I understand')}
                    />
                  </label>
                </div>
                <label className='space-y-2'>
                  <span className='text-sm font-medium'>
                    {t('Popup content')}
                  </span>
                  <Textarea
                    value={rule.popup_content}
                    onChange={(event) =>
                      updateRule(index, { popup_content: event.target.value })
                    }
                    rows={4}
                    placeholder={t('Explain the regional access policy.')}
                  />
                </label>
                <label className='space-y-2'>
                  <span className='text-sm font-medium'>
                    {t('Checkbox text')}
                  </span>
                  <Input
                    value={rule.popup_notice}
                    onChange={(event) =>
                      updateRule(index, { popup_notice: event.target.value })
                    }
                    placeholder={t('I have read and understood')}
                  />
                </label>
                <label className='space-y-2'>
                  <span className='text-sm font-medium'>
                    {t('Popup version')}
                  </span>
                  <Input
                    value={rule.version}
                    onChange={(event) =>
                      updateRule(index, { version: event.target.value })
                    }
                    placeholder='v1'
                  />
                </label>
              </div>
            )}
          </div>
        ))}
      </div>

      <div className='flex flex-wrap gap-2'>
        <Button
          type='button'
          variant='outline'
          onClick={() => setRules((previous) => [...previous, emptyRule()])}
        >
          <Plus className='size-4' />
          {t('Add region rule')}
        </Button>
        <Button
          type='button'
          onClick={handleSave}
          disabled={updateOptions.isPending}
        >
          <Save className='size-4' />
          {updateOptions.isPending ? t('Saving...') : t('Save Changes')}
        </Button>
      </div>
    </SettingsSection>
  )
}
