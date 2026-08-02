import { memo, useCallback, useState } from 'react'
import { type UseFormReturn } from 'react-hook-form'
import { Code2, Eye } from 'lucide-react'
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
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { AmountDiscountVisualEditor } from '../integrations/amount-discount-visual-editor'
import { GroupRatioVisualEditor } from './group-ratio-visual-editor'

type GroupFormValues = {
  GroupRatio: string
  TopupGroupRatio: string
  UserUsableGroups: string
  GroupGroupRatio: string
  AutoGroups: string
  DefaultUseAutoGroup: boolean
  GroupSpecialUsableGroup: string
  AmountDiscount: string
}

type GroupRatioFormProps = {
  form: UseFormReturn<GroupFormValues>
  onSave: (values: GroupFormValues) => Promise<void>
  isSaving: boolean
}

export const GroupRatioForm = memo(function GroupRatioForm({
  form,
  onSave,
  isSaving,
}: GroupRatioFormProps) {
  const { t } = useTranslation()
  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const groupRatio = form.watch('GroupRatio')

  const handleFieldChange = useCallback(
    (field: keyof GroupFormValues, value: string) => {
      form.setValue(field, value, {
        shouldValidate: true,
        shouldDirty: true,
      })
    },
    [form]
  )

  const toggleEditMode = useCallback(() => {
    setEditMode((prev) => (prev === 'visual' ? 'json' : 'visual'))
  }, [])

  return (
    <div className='space-y-6'>
      <div className='flex justify-end'>
        <Button variant='outline' size='sm' onClick={toggleEditMode}>
          {editMode === 'visual' ? (
            <>
              <Code2 className='mr-2 h-4 w-4' />
              {t('Switch to JSON')}
            </>
          ) : (
            <>
              <Eye className='mr-2 h-4 w-4' />
              {t('Switch to Visual')}
            </>
          )}
        </Button>
      </div>

      <Form {...form}>
        {editMode === 'visual' ? (
          <div className='space-y-6'>
            <Tabs defaultValue='customer-tiers' className='space-y-6'>
              <TabsList className='grid w-full grid-cols-3'>
                <TabsTrigger value='customer-tiers'>
                  {t('Customer tiers')}
                </TabsTrigger>
                <TabsTrigger value='token-groups'>
                  {t('Token groups')}
                </TabsTrigger>
                <TabsTrigger value='recharge-tiers'>
                  {t('Recharge amount tiers')}
                </TabsTrigger>
              </TabsList>

              <TabsContent value='customer-tiers' className='space-y-6'>
                <GroupRatioVisualEditor
                  view='customer'
                  groupRatio={groupRatio}
                  topupGroupRatio={form.watch('TopupGroupRatio')}
                  userUsableGroups={form.watch('UserUsableGroups')}
                  groupGroupRatio={form.watch('GroupGroupRatio')}
                  groupSpecialUsableGroup={form.watch(
                    'GroupSpecialUsableGroup'
                  )}
                  autoGroups={form.watch('AutoGroups')}
                  onChange={(field, value) =>
                    handleFieldChange(field as keyof GroupFormValues, value)
                  }
                />
              </TabsContent>

              <TabsContent value='token-groups' className='space-y-6'>
                <GroupRatioVisualEditor
                  view='token'
                  groupRatio={groupRatio}
                  topupGroupRatio={form.watch('TopupGroupRatio')}
                  userUsableGroups={form.watch('UserUsableGroups')}
                  groupGroupRatio={form.watch('GroupGroupRatio')}
                  groupSpecialUsableGroup={form.watch(
                    'GroupSpecialUsableGroup'
                  )}
                  autoGroups={form.watch('AutoGroups')}
                  onChange={(field, value) =>
                    handleFieldChange(field as keyof GroupFormValues, value)
                  }
                />
                <FormField
                  control={form.control}
                  name='DefaultUseAutoGroup'
                  render={({ field }) => (
                    <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                      <div className='space-y-0.5'>
                        <FormLabel className='text-base'>
                          {t('Default to auto groups')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'When enabled, newly created tokens start in the first auto group.'
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
              </TabsContent>

              <TabsContent value='recharge-tiers'>
                <div className='space-y-4 rounded-lg border p-4'>
                  <div>
                    <h3 className='font-semibold'>
                      {t('Recharge amount tiers')}
                    </h3>
                    <p className='text-muted-foreground text-sm'>
                      {t(
                        'Amount discounts are combined with the customer tier recharge discount. A multiplier of 0.9 means 10% off.'
                      )}
                    </p>
                  </div>
                  <AmountDiscountVisualEditor
                    value={form.watch('AmountDiscount')}
                    onChange={(value) =>
                      handleFieldChange('AmountDiscount', value)
                    }
                  />
                </div>
              </TabsContent>
            </Tabs>

            <Button onClick={form.handleSubmit(onSave)} disabled={isSaving}>
              {isSaving
                ? t('Saving...')
                : t('Save customer tiers and discounts')}
            </Button>
          </div>
        ) : (
          <form onSubmit={form.handleSubmit(onSave)} className='space-y-6'>
            <FormField
              control={form.control}
              name='GroupRatio'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Group ratios')}</FormLabel>
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
                      'JSON map of group → ratio applied when the user selects the group explicitly.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='TopupGroupRatio'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Top-up group ratios')}</FormLabel>
                  <FormControl>
                    <JsonCodeEditor
                      value={field.value}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      textareaRef={field.ref}
                      heightClassName='h-40 min-h-40 max-h-40'
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Optional multiplier per user group used when calculating recharge pricing. Provide a JSON object such as'
                    )}
                    {` { "default": 1, "vip": 1.2 }`}.
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='UserUsableGroups'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Selectable groups')}</FormLabel>
                  <FormControl>
                    <JsonCodeEditor
                      value={field.value}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      textareaRef={field.ref}
                      heightClassName='h-40 min-h-40 max-h-40'
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'JSON map of group → description exposed when users create API keys.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AmountDiscount'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Recharge amount tiers')}</FormLabel>
                  <FormControl>
                    <JsonCodeEditor
                      value={field.value}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      textareaRef={field.ref}
                      heightClassName='h-40 min-h-40 max-h-40'
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Discount map by recharge amount (JSON object)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='GroupGroupRatio'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Inter-group overrides')}</FormLabel>
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
                    {t('Nested JSON: source group →')}{' '}
                    {`{ targetGroup: ratio }`}{' '}
                    {t(
                      'to override billing when a user in one group uses a token of another group.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AutoGroups'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Auto assignment order')}</FormLabel>
                  <FormControl>
                    <JsonCodeEditor
                      value={field.value}
                      onChange={field.onChange}
                      name={field.name}
                      onBlur={field.onBlur}
                      textareaRef={field.ref}
                      heightClassName='h-40 min-h-40 max-h-40'
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'JSON array of group identifiers. When enabled below, new tokens rotate through this list.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='GroupSpecialUsableGroup'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Special usable group rules')}</FormLabel>
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
                      'Nested JSON defining per-group rules for adding (+:), removing (-:), or appending usable groups.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='DefaultUseAutoGroup'
              render={({ field }) => (
                <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                  <div className='space-y-0.5'>
                    <FormLabel className='text-base'>
                      {t('Default to auto groups')}
                    </FormLabel>
                    <FormDescription>
                      {t(
                        'When enabled, newly created tokens start in the first auto group.'
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

            <Button type='submit' disabled={isSaving}>
              {isSaving
                ? t('Saving...')
                : t('Save customer tiers and discounts')}
            </Button>
          </form>
        )}
      </Form>
    </div>
  )
})
