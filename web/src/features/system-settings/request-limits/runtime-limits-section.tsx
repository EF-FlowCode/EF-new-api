import { useEffect } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
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
import { Switch } from '@/components/ui/switch'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOptions } from '../hooks/use-update-option'

// eflow: 原先只能用环境变量配置的运行参数。范围与后端 model/option.go 的校验一致。
const OPTION_KEYS = {
  streamingTimeoutSeconds: 'runtime_limit_setting.streaming_timeout_seconds',
  errorLogEnabled: 'runtime_limit_setting.error_log_enabled',
  taskTimeoutMinutes: 'runtime_limit_setting.task_timeout_minutes',
  maxRequestBodyMB: 'runtime_limit_setting.max_request_body_mb',
  notifyLimitCount: 'runtime_limit_setting.notify_limit_count',
  notifyLimitDurationMinutes:
    'runtime_limit_setting.notify_limit_duration_minutes',
  responseHeaderTimeoutSeconds:
    'runtime_limit_setting.response_header_timeout_seconds',
  nonStreamResponseHeaderTimeoutSeconds:
    'runtime_limit_setting.non_stream_response_header_timeout_seconds',
} as const

const runtimeLimitsSchema = z.object({
  streamingTimeoutSeconds: z.coerce.number().int().min(30).max(3600),
  errorLogEnabled: z.boolean(),
  taskTimeoutMinutes: z.coerce.number().int().min(0).max(43200),
  maxRequestBodyMB: z.coerce.number().int().min(1).max(1024),
  notifyLimitCount: z.coerce.number().int().min(0).max(1000),
  notifyLimitDurationMinutes: z.coerce.number().int().min(1).max(10080),
  responseHeaderTimeoutSeconds: z.coerce.number().int().min(0).max(7200),
  nonStreamResponseHeaderTimeoutSeconds: z.coerce
    .number()
    .int()
    .min(0)
    .max(7200),
})

type RuntimeLimitsFormValues = z.output<typeof runtimeLimitsSchema>
type RuntimeLimitsFormInput = z.input<typeof runtimeLimitsSchema>
type NumberFieldName = Exclude<keyof RuntimeLimitsFormValues, 'errorLogEnabled'>

type RuntimeLimitsSectionProps = {
  defaultValues: RuntimeLimitsFormValues
}

export function RuntimeLimitsSection({
  defaultValues,
}: RuntimeLimitsSectionProps) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()
  const form = useForm<
    RuntimeLimitsFormInput,
    unknown,
    RuntimeLimitsFormValues
  >({
    resolver: zodResolver(runtimeLimitsSchema),
    defaultValues: defaultValues as RuntimeLimitsFormInput,
  })

  useEffect(() => {
    form.reset(defaultValues as RuntimeLimitsFormInput)
  }, [defaultValues, form])

  const onSubmit = async (values: RuntimeLimitsFormValues) => {
    const updates = (
      Object.keys(OPTION_KEYS) as Array<keyof RuntimeLimitsFormValues>
    )
      .filter((name) => values[name] !== defaultValues[name])
      .map((name) => ({ key: OPTION_KEYS[name], value: values[name] }))

    if (updates.length === 0) return
    await updateOptions.mutateAsync(updates)
  }

  const numberFields: Array<{
    name: NumberFieldName
    label: string
    description: string
    min: number
    max: number
  }> = [
    {
      name: 'streamingTimeoutSeconds',
      label: t('Streaming timeout (seconds)'),
      description: t(
        'Abort a streaming request when the upstream sends no data for this long. Reasoning models can stay silent for minutes, so avoid values below 150.'
      ),
      min: 30,
      max: 3600,
    },
    {
      name: 'responseHeaderTimeoutSeconds',
      label: t('Response header timeout (seconds)'),
      description: t(
        'How long streaming requests and other outbound calls wait for the upstream to start responding. Streaming upstreams answer within seconds, so this catches hung upstreams. 0 disables the limit.'
      ),
      min: 0,
      max: 7200,
    },
    {
      name: 'nonStreamResponseHeaderTimeoutSeconds',
      label: t('Non-streaming response timeout (seconds)'),
      description: t(
        'How long non-streaming requests wait for the upstream response. These upstreams reply only after the whole result is ready, so slow work such as image generation needs a longer limit. 0 disables the limit.'
      ),
      min: 0,
      max: 7200,
    },
    {
      name: 'taskTimeoutMinutes',
      label: t('Async task timeout (minutes)'),
      description: t(
        'Unfinished async tasks such as video generation are marked failed and refunded after this long. 0 disables the timeout.'
      ),
      min: 0,
      max: 43200,
    },
    {
      name: 'maxRequestBodyMB',
      label: t('Max request body (MB)'),
      description: t(
        'Largest request body accepted after decompression. Larger requests are rejected.'
      ),
      min: 1,
      max: 1024,
    },
    {
      name: 'notifyLimitCount',
      label: t('Notifications per window'),
      description: t(
        'Maximum notifications of the same type sent to one user within the window. 0 stops these notifications.'
      ),
      min: 0,
      max: 1000,
    },
    {
      name: 'notifyLimitDurationMinutes',
      label: t('Notification window (minutes)'),
      description: t(
        'Length of the window used to limit repeated notifications.'
      ),
      min: 1,
      max: 10080,
    },
  ]

  return (
    <SettingsSection
      title={t('Runtime Limits')}
      description={t(
        'Values start from the server environment variables; saved values take effect within one sync interval and override them.'
      )}
    >
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='errorLogEnabled'
            render={({ field }) => (
              <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                <div className='space-y-0.5'>
                  <FormLabel className='text-base'>
                    {t('Record error logs')}
                  </FormLabel>
                  <FormDescription>
                    {t(
                      'Save failed relay requests to the usage logs for troubleshooting.'
                    )}
                  </FormDescription>
                </div>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </FormItem>
            )}
          />

          {numberFields.map((item) => (
            <FormField
              key={item.name}
              control={form.control}
              name={item.name}
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{item.label}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      min={item.min}
                      max={item.max}
                      className='w-32'
                      value={
                        field.value === undefined || field.value === null
                          ? ''
                          : String(field.value)
                      }
                      onChange={(event) => field.onChange(event.target.value)}
                      onBlur={field.onBlur}
                      name={field.name}
                      ref={field.ref}
                    />
                  </FormControl>
                  <FormDescription>{item.description}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          ))}

          <Button type='submit' disabled={updateOptions.isPending}>
            {updateOptions.isPending
              ? t('Saving...')
              : t('Save runtime limits')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
