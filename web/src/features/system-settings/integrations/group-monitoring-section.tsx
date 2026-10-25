import { useEffect, useMemo, useState } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { TagInput } from '@/components/tag-input'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOptions } from '../hooks/use-update-option'

// ----------------------------------------------------------------------------
// Schema
// ----------------------------------------------------------------------------

const monitoringSchema = z.object({
  monitoring_groups: z.array(z.string()),
  availability_period_minutes: z.number().int().min(1),
  cache_hit_period_minutes: z.number().int().min(1),
  aggregation_interval_minutes: z.number().int().min(1),
  availability_exclude_models: z.array(z.string()),
  cache_hit_exclude_models: z.array(z.string()),
  availability_exclude_keywords: z.array(z.string()),
  availability_exclude_status_codes: z.array(z.string().regex(/^[1-5]\d{2}$/)),
  cache_tokens_separate_groups: z.array(z.string()),
})

type GroupMonitoringFormValues = z.infer<typeof monitoringSchema>

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

const FIELD_KEYS = {
  monitoring_groups: 'group_monitoring_setting.monitoring_groups',
  group_display_order: 'group_monitoring_setting.group_display_order',
  availability_period_minutes:
    'group_monitoring_setting.availability_period_minutes',
  cache_hit_period_minutes: 'group_monitoring_setting.cache_hit_period_minutes',
  aggregation_interval_minutes:
    'group_monitoring_setting.aggregation_interval_minutes',
  availability_exclude_models:
    'group_monitoring_setting.availability_exclude_models',
  cache_hit_exclude_models: 'group_monitoring_setting.cache_hit_exclude_models',
  availability_exclude_keywords:
    'group_monitoring_setting.availability_exclude_keywords',
  availability_exclude_status_codes:
    'group_monitoring_setting.availability_exclude_status_codes',
  cache_tokens_separate_groups:
    'group_monitoring_setting.cache_tokens_separate_groups',
} as const

function parseArrayField(value: string | undefined): string[] {
  if (!value || value === '[]' || value === 'null') return []
  let arr: unknown
  try {
    const parsed = JSON.parse(value)
    arr = Array.isArray(parsed) ? parsed : []
  } catch {
    arr = value.split(',')
  }
  if (!Array.isArray(arr)) return []
  return arr
    .map((v) => String(v).trim())
    .filter((v) => v && v !== 'null' && v !== 'undefined')
}

function toJsonArrayString(arr: string[] | undefined): string {
  const cleaned = (arr || []).map((v) => String(v).trim()).filter(Boolean)
  return JSON.stringify(cleaned)
}

// ----------------------------------------------------------------------------
// Props
// ----------------------------------------------------------------------------

export type GroupMonitoringSettingsSectionProps = {
  defaultValues: {
    'group_monitoring_setting.monitoring_groups': string
    'group_monitoring_setting.group_display_order': string
    'group_monitoring_setting.availability_period_minutes': number
    'group_monitoring_setting.cache_hit_period_minutes': number
    'group_monitoring_setting.aggregation_interval_minutes': number
    'group_monitoring_setting.availability_exclude_models': string
    'group_monitoring_setting.cache_hit_exclude_models': string
    'group_monitoring_setting.availability_exclude_keywords': string
    'group_monitoring_setting.availability_exclude_status_codes': string
    'group_monitoring_setting.cache_tokens_separate_groups': string
  }
}

// ----------------------------------------------------------------------------
// Component
// ----------------------------------------------------------------------------

export function GroupMonitoringSettingsSection({
  defaultValues,
}: GroupMonitoringSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()
  const [availableGroups, setAvailableGroups] = useState<string[]>([])
  const [refreshing, setRefreshing] = useState(false)

  const formDefaults = useMemo<GroupMonitoringFormValues>(
    () => ({
      monitoring_groups: parseArrayField(
        defaultValues[FIELD_KEYS.monitoring_groups]
      ),
      availability_period_minutes:
        defaultValues[FIELD_KEYS.availability_period_minutes] || 60,
      cache_hit_period_minutes:
        defaultValues[FIELD_KEYS.cache_hit_period_minutes] || 60,
      aggregation_interval_minutes:
        defaultValues[FIELD_KEYS.aggregation_interval_minutes] || 5,
      availability_exclude_models: parseArrayField(
        defaultValues[FIELD_KEYS.availability_exclude_models]
      ),
      cache_hit_exclude_models: parseArrayField(
        defaultValues[FIELD_KEYS.cache_hit_exclude_models]
      ),
      availability_exclude_keywords: parseArrayField(
        defaultValues[FIELD_KEYS.availability_exclude_keywords]
      ),
      availability_exclude_status_codes: parseArrayField(
        defaultValues[FIELD_KEYS.availability_exclude_status_codes]
      ),
      cache_tokens_separate_groups: parseArrayField(
        defaultValues[FIELD_KEYS.cache_tokens_separate_groups]
      ),
    }),
    [defaultValues]
  )

  const form = useForm<GroupMonitoringFormValues>({
    resolver: zodResolver(monitoringSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  // Fetch available groups for the multi-select
  useEffect(() => {
    let active = true
    api
      .get('/api/group/', CALLER_HANDLED_REQUEST_CONFIG)
      .then((res) => {
        if (!active) return
        if (res.data?.success) {
          const data = res.data.data
          const groups: string[] = (
            Array.isArray(data) ? data : Object.keys(data || {})
          ).filter((g: string) => g !== 'auto')
          setAvailableGroups(groups)
        }
      })
      .catch(() => {
        // ignore
      })
    return () => {
      active = false
    }
  }, [])

  const onSubmit = async (values: GroupMonitoringFormValues) => {
    const monitoringGroupsJson = toJsonArrayString(values.monitoring_groups)
    const updates: Array<{ key: string; value: string | number }> = [
      { key: FIELD_KEYS.monitoring_groups, value: monitoringGroupsJson },
      { key: FIELD_KEYS.group_display_order, value: monitoringGroupsJson },
      {
        key: FIELD_KEYS.availability_period_minutes,
        value: String(values.availability_period_minutes),
      },
      {
        key: FIELD_KEYS.cache_hit_period_minutes,
        value: String(values.cache_hit_period_minutes),
      },
      {
        key: FIELD_KEYS.aggregation_interval_minutes,
        value: String(values.aggregation_interval_minutes),
      },
      {
        key: FIELD_KEYS.availability_exclude_models,
        value: toJsonArrayString(values.availability_exclude_models),
      },
      {
        key: FIELD_KEYS.cache_hit_exclude_models,
        value: toJsonArrayString(values.cache_hit_exclude_models),
      },
      {
        key: FIELD_KEYS.availability_exclude_keywords,
        value: toJsonArrayString(values.availability_exclude_keywords),
      },
      {
        key: FIELD_KEYS.availability_exclude_status_codes,
        value: JSON.stringify(
          values.availability_exclude_status_codes.map(Number)
        ),
      },
      {
        key: FIELD_KEYS.cache_tokens_separate_groups,
        value: toJsonArrayString(values.cache_tokens_separate_groups),
      },
    ]
    await updateOptions.mutateAsync(updates)
  }

  const handleRefreshNow = async () => {
    setRefreshing(true)
    try {
      const res = await api.post(
        '/api/monitoring/admin/refresh',
        undefined,
        CALLER_HANDLED_REQUEST_CONFIG
      )
      if (res.data?.success) {
        toast.success(t('Refreshed successfully'))
      } else {
        toast.error(res.data?.message || t('Refresh failed'))
      }
    } catch {
      toast.error(t('Refresh failed'))
    } finally {
      setRefreshing(false)
    }
  }

  return (
    <SettingsSection
      title={t('Group Monitoring')}
      description={t(
        'Configure which groups to monitor on the dashboard, and how availability and cache hit rates are computed.'
      )}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='monitoring_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Monitored groups')}</FormLabel>
                <FormControl>
                  <TagInput
                    value={field.value}
                    onChange={(next) =>
                      field.onChange(
                        next.filter((g) =>
                          availableGroups.length === 0
                            ? true
                            : availableGroups.includes(g)
                        )
                      )
                    }
                    placeholder={t('Select groups to monitor')}
                  />
                </FormControl>
                <FormDescription>
                  {availableGroups.length === 0
                    ? t('Loading available groups…')
                    : t('Available groups: {{groups}}', {
                        groups: availableGroups.join(', '),
                      })}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-4 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='availability_period_minutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Availability period (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      value={field.value}
                      onChange={(e) =>
                        field.onChange(Number(e.target.value) || 60)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Window for computing channel availability')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='cache_hit_period_minutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Cache hit period (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      value={field.value}
                      onChange={(e) =>
                        field.onChange(Number(e.target.value) || 60)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Window for computing cache hit ratio')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='aggregation_interval_minutes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Aggregation interval (minutes)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      value={field.value}
                      onChange={(e) =>
                        field.onChange(Number(e.target.value) || 5)
                      }
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Bucket size for historical aggregation')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='availability_exclude_models'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Exclude models from availability stats')}
                  </FormLabel>
                  <FormControl>
                    <TagInput
                      value={field.value}
                      onChange={field.onChange}
                      placeholder={t('Type and press enter to add')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('These models do not count toward availability')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='cache_hit_exclude_models'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Exclude models from cache hit stats')}
                  </FormLabel>
                  <FormControl>
                    <TagInput
                      value={field.value}
                      onChange={field.onChange}
                      placeholder={t('Type and press enter to add')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('These models do not count toward cache hit ratio')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-4 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='availability_exclude_keywords'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Exclude error message keywords')}</FormLabel>
                  <FormControl>
                    <TagInput
                      value={field.value}
                      onChange={field.onChange}
                      placeholder={t('Type and press enter to add')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      "Errors whose message contains any of these don't count as unavailable"
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name='availability_exclude_status_codes'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Exclude HTTP status codes')}</FormLabel>
                  <FormControl>
                    <TagInput
                      value={field.value}
                      onChange={field.onChange}
                      placeholder={t('e.g. 400, 503')}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      "Errors with these HTTP status codes don't count as unavailable"
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='cache_tokens_separate_groups'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Cache tokens separate groups')}</FormLabel>
                <FormControl>
                  <TagInput
                    value={field.value}
                    onChange={field.onChange}
                    placeholder={t('Type and press enter to add')}
                  />
                </FormControl>
                <FormDescription>
                  {t('Cache tokens for these groups are tracked separately')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='flex flex-wrap items-center gap-2'>
            <Button type='submit' disabled={updateOptions.isPending}>
              {t('Save monitoring settings')}
            </Button>
            <Button
              type='button'
              variant='outline'
              onClick={handleRefreshNow}
              disabled={refreshing}
            >
              <RefreshCw
                className={cn(
                  'mr-1.5 h-3.5 w-3.5',
                  refreshing && 'animate-spin'
                )}
              />
              {t('Refresh monitoring data now')}
            </Button>
          </div>
        </form>
      </Form>
    </SettingsSection>
  )
}
