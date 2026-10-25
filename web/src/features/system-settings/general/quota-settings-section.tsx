import * as z from 'zod'
import type { Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { formatQuota } from '@/lib/format'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
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
import { FormDirtyIndicator } from '../components/form-dirty-indicator'
import { FormNavigationGuard } from '../components/form-navigation-guard'
import { SettingsSection } from '../components/settings-section'
import { useSettingsForm } from '../hooks/use-settings-form'
import { useUpdateOptions } from '../hooks/use-update-option'

const quotaSchema = z.object({
  QuotaForNewUser: z.coerce.number().min(0),
  PreConsumedQuota: z.coerce.number().min(0),
  QuotaForInviter: z.coerce.number().min(0),
  QuotaForInvitee: z.coerce.number().min(0),
  TopUpLink: z.string().url().optional().or(z.literal('')),
  'general_setting.docs_link': z.string().url().optional().or(z.literal('')),
  // 这两条用 .optional() 不是因为字段可空，是因为 zod 校验拿到的 RHF
  // state 是 expandDotPaths 后的嵌套对象，字面量 'a.b' 键查不到值，必失败。
  // 提交逻辑走 collectDirtyValues 用扁平点路径，跟 backend 对齐，不受
  // optional 影响。同套问题 oauth-section 的 'discord.enabled' 之类也存在。
  'quota_setting.enable_free_model_pre_consume': z.boolean().optional(),
  AffiliateEnabled: z.boolean().optional(),
})

type QuotaFormValues = z.infer<typeof quotaSchema>

type QuotaSettingsSectionProps = {
  defaultValues: QuotaFormValues
}

export function QuotaSettingsSection({
  defaultValues,
}: QuotaSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()

  const { form, handleSubmit, isDirty, isSubmitting } =
    useSettingsForm<QuotaFormValues>({
      resolver: zodResolver(quotaSchema) as Resolver<
        QuotaFormValues,
        unknown,
        QuotaFormValues
      >,
      defaultValues,
      onSubmit: async (_data, changedFields) => {
        const updates = Object.entries(changedFields).map(([key, value]) => ({
          key,
          value: value as string | number | boolean,
        }))
        if (updates.length === 0) return
        await updateOptions.mutateAsync(updates)
      },
    })

  return (
    <SettingsSection
      title={t('Quota Settings')}
      description={t('Configure user quota allocation and rewards')}
    >
      <FormNavigationGuard when={isDirty} />

      <Form {...form}>
        <form onSubmit={handleSubmit} className='space-y-6'>
          <FormDirtyIndicator isDirty={isDirty} />

          <div className='grid items-start gap-6 xl:grid-cols-2'>
            <Card>
              <CardHeader>
                <CardTitle>{t('User quota policy')}</CardTitle>
                <CardDescription>
                  {t('Configure initial quota and request pre-consumption.')}
                </CardDescription>
              </CardHeader>
              <CardContent className='space-y-5'>
                <div className='grid gap-5 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='QuotaForNewUser'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('New User Quota')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            step={1}
                            value={field.value as number}
                            onChange={(e) =>
                              field.onChange(e.target.valueAsNumber)
                            }
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Current equivalent: {{amount}}', {
                            amount: formatQuota(Number(field.value) || 0),
                          })}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='PreConsumedQuota'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Pre-Consumed Quota')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            step={1}
                            value={field.value as number}
                            onChange={(e) =>
                              field.onChange(e.target.valueAsNumber)
                            }
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Current equivalent: {{amount}}', {
                            amount: formatQuota(Number(field.value) || 0),
                          })}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>

                <FormField
                  control={form.control}
                  name='quota_setting.enable_free_model_pre_consume'
                  render={({ field }) => (
                    <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                      <div className='space-y-0.5'>
                        <FormLabel className='text-base'>
                          {t('Pre-Consume for Free Models')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'When enabled, zero-cost models also pre-consume quota before final settlement.'
                          )}
                        </FormDescription>
                      </div>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          disabled={updateOptions.isPending}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>

            <Card>
              <CardHeader className='flex flex-row items-start justify-between gap-4'>
                <div className='space-y-1.5'>
                  <CardTitle>{t('Referral rewards')}</CardTitle>
                  <CardDescription>
                    {t('Configure referral program state and reward amounts.')}
                  </CardDescription>
                </div>
                <FormField
                  control={form.control}
                  name='AffiliateEnabled'
                  render={({ field }) => (
                    <FormItem className='flex items-center gap-2 space-y-0'>
                      <FormLabel className='text-sm whitespace-nowrap'>
                        {t('Enable referral program')}
                      </FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          disabled={updateOptions.isPending}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />
              </CardHeader>
              <CardContent className='space-y-5'>
                <div className='grid gap-5 sm:grid-cols-2'>
                  <FormField
                    control={form.control}
                    name='QuotaForInviter'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Inviter Reward')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            step={1}
                            value={field.value as number}
                            onChange={(e) =>
                              field.onChange(e.target.valueAsNumber)
                            }
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Current equivalent: {{amount}}', {
                            amount: formatQuota(Number(field.value) || 0),
                          })}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='QuotaForInvitee'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Invitee Reward')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            step={1}
                            value={field.value as number}
                            onChange={(e) =>
                              field.onChange(e.target.valueAsNumber)
                            }
                            name={field.name}
                            onBlur={field.onBlur}
                            ref={field.ref}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Current equivalent: {{amount}}', {
                            amount: formatQuota(Number(field.value) || 0),
                          })}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>

                <div className='rounded-md border border-amber-300 bg-amber-50 p-3 text-sm dark:border-amber-900 dark:bg-amber-950/30'>
                  {t(
                    'When disabled, the referral entry is hidden and new rewards stop. Existing reward balances are retained.'
                  )}
                </div>
              </CardContent>
            </Card>

            <Card className='xl:col-span-2'>
              <CardHeader>
                <CardTitle>{t('External links')}</CardTitle>
                <CardDescription>
                  {t('Configure the recharge and documentation destinations.')}
                </CardDescription>
              </CardHeader>
              <CardContent className='grid gap-5 md:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='TopUpLink'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Top-Up Link')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('https://example.com/topup')}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('External link for users to purchase quota')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='general_setting.docs_link'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Documentation Link')}</FormLabel>
                      <FormControl>
                        <Input
                          placeholder={t('https://docs.example.com')}
                          {...field}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Link to your documentation site')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </CardContent>
            </Card>
          </div>

          <div
            className={`bg-background/95 flex items-center justify-between gap-4 rounded-lg border p-3 ${
              isDirty ? 'sticky bottom-4 z-10 shadow-sm backdrop-blur' : ''
            }`}
          >
            <p className='text-muted-foreground text-sm'>
              {isDirty
                ? t('You have unsaved changes')
                : t('All changes are saved')}
            </p>
            <Button
              type='submit'
              disabled={updateOptions.isPending || isSubmitting || !isDirty}
            >
              {updateOptions.isPending ? t('Saving...') : t('Save Changes')}
            </Button>
          </div>
        </form>
      </Form>
    </SettingsSection>
  )
}
