import { useEffect, useRef } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { JsonCodeEditor } from '@/components/json-code-editor'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOptions } from '../hooks/use-update-option'
import {
  formatJsonForTextarea,
  normalizeJsonString,
  validateJsonString,
} from './utils'

const schema = z.object({
  claude: z.object({
    model_headers_settings: z.string().superRefine((value, ctx) => {
      const result = validateJsonString(value)
      if (!result.valid) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: result.message || 'Invalid JSON',
        })
      }
    }),
    default_max_tokens: z.string().superRefine((value, ctx) => {
      const result = validateJsonString(value)
      if (!result.valid) {
        ctx.addIssue({
          code: z.ZodIssueCode.custom,
          message: result.message || 'Invalid JSON',
        })
      }
    }),
    thinking_adapter_enabled: z.boolean(),
    thinking_adapter_budget_tokens_percentage: z.coerce
      .number()
      .min(0.1, { message: 'Must be at least 0.1' })
      .max(1, { message: 'Must be 1 or less' }),
  }),
})

type ClaudeSettingsFormValues = z.output<typeof schema>
type ClaudeSettingsFormInput = z.input<typeof schema>

type FlatClaudeSettings = {
  'claude.model_headers_settings': string
  'claude.default_max_tokens': string
  'claude.thinking_adapter_enabled': boolean
  'claude.thinking_adapter_budget_tokens_percentage': number
}

type ClaudeSettingsCardProps = {
  defaultValues: ClaudeSettingsFormInput
}

export function ClaudeSettingsCard({ defaultValues }: ClaudeSettingsCardProps) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()
  const normalizedDefaultsRef = useRef<FlatClaudeSettings>({
    'claude.model_headers_settings': normalizeJsonString(
      defaultValues.claude.model_headers_settings
    ),
    'claude.default_max_tokens': normalizeJsonString(
      defaultValues.claude.default_max_tokens
    ),
    'claude.thinking_adapter_enabled':
      defaultValues.claude.thinking_adapter_enabled,
    'claude.thinking_adapter_budget_tokens_percentage': Number(
      defaultValues.claude.thinking_adapter_budget_tokens_percentage
    ),
  })

  const buildFormDefaults = (
    values: ClaudeSettingsFormInput
  ): ClaudeSettingsFormInput => ({
    claude: {
      model_headers_settings: formatJsonForTextarea(
        values.claude.model_headers_settings
      ),
      default_max_tokens: formatJsonForTextarea(
        values.claude.default_max_tokens
      ),
      thinking_adapter_enabled: values.claude.thinking_adapter_enabled,
      thinking_adapter_budget_tokens_percentage:
        values.claude.thinking_adapter_budget_tokens_percentage,
    },
  })

  const form = useForm<
    ClaudeSettingsFormInput,
    unknown,
    ClaudeSettingsFormValues
  >({
    resolver: zodResolver(schema),
    defaultValues: buildFormDefaults(defaultValues),
  })

  useEffect(() => {
    normalizedDefaultsRef.current = {
      'claude.model_headers_settings': normalizeJsonString(
        defaultValues.claude.model_headers_settings
      ),
      'claude.default_max_tokens': normalizeJsonString(
        defaultValues.claude.default_max_tokens
      ),
      'claude.thinking_adapter_enabled':
        defaultValues.claude.thinking_adapter_enabled,
      'claude.thinking_adapter_budget_tokens_percentage': Number(
        defaultValues.claude.thinking_adapter_budget_tokens_percentage
      ),
    }

    form.reset(buildFormDefaults(defaultValues))
  }, [defaultValues, form])

  const onSubmit = async (values: ClaudeSettingsFormValues) => {
    const normalized: FlatClaudeSettings = {
      'claude.model_headers_settings': normalizeJsonString(
        values.claude.model_headers_settings
      ),
      'claude.default_max_tokens': normalizeJsonString(
        values.claude.default_max_tokens
      ),
      'claude.thinking_adapter_enabled': values.claude.thinking_adapter_enabled,
      'claude.thinking_adapter_budget_tokens_percentage':
        values.claude.thinking_adapter_budget_tokens_percentage,
    }

    const updates = (
      Object.keys(normalized) as Array<keyof FlatClaudeSettings>
    ).filter((key) => normalized[key] !== normalizedDefaultsRef.current[key])

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    await updateOptions.mutateAsync(
      updates.map((key) => ({ key, value: normalized[key] }))
    )
  }

  return (
    <SettingsSection
      title={t('Claude')}
      description={t(
        'Override Anthropic headers, defaults, and thinking adapter behavior'
      )}
    >
      <Form {...form}>
        {/* eslint-disable-next-line react-hooks/refs */}
        <form onSubmit={form.handleSubmit(onSubmit)} className='space-y-6'>
          <FormField
            control={form.control}
            name='claude.model_headers_settings'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Request Header Overrides')}</FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Provide per-model header overrides as JSON. Useful for enabling beta features such as expanded context windows.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='claude.default_max_tokens'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Default Max Tokens')}</FormLabel>
                <FormControl>
                  <JsonCodeEditor
                    value={field.value}
                    onChange={field.onChange}
                    name={field.name}
                    onBlur={field.onBlur}
                    textareaRef={field.ref}
                  />
                </FormControl>
                <FormDescription>
                  {t('Example')}{' '}
                  {`{ "default": 8192, "claude-3-haiku-20240307": 4096, "claude-3-opus-20240229": 4096, "claude-3-7-sonnet-20250219-thinking": 8192 }`}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='space-y-4 rounded-lg border p-4'>
            <FormField
              control={form.control}
              name='claude.thinking_adapter_enabled'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Thinking Adapter')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'Translate `-thinking` suffixes into Anthropic native thinking models while keeping pricing predictable.'
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

            <FormField
              control={form.control}
              name='claude.thinking_adapter_budget_tokens_percentage'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Budget Tokens Ratio')}</FormLabel>
                  <FormControl>
                    <Input
                      {...field}
                      value={String(field.value ?? '')}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Budget tokens = max tokens × ratio. Accepts a decimal between 0.1 and 1.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <Button type='submit' disabled={updateOptions.isPending}>
            {updateOptions.isPending ? t('Saving...') : t('Save Changes')}
          </Button>
        </form>
      </Form>
    </SettingsSection>
  )
}
