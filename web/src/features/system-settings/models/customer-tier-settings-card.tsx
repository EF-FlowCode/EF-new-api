import { useEffect } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { updateSystemOptionsBulk } from '../api'
import { SettingsSection } from '../components/settings-section'
import { GroupRatioForm } from './group-ratio-form'
import {
  formatJsonForTextarea,
  normalizeJsonString,
  validateJsonString,
} from './utils'

const jsonString = z.string().superRefine((value, ctx) => {
  const result = validateJsonString(value)
  if (!result.valid) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: result.message || 'Invalid JSON',
    })
  }
})

const jsonObjectString = z.string().superRefine((value, ctx) => {
  const result = validateJsonString(value, {
    predicate: (parsed) =>
      parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed),
    predicateMessage: 'Expected a JSON object',
  })
  if (!result.valid) {
    ctx.addIssue({
      code: z.ZodIssueCode.custom,
      message: result.message || 'Invalid JSON object',
    })
  }
})

const customerTierSchema = z.object({
  GroupRatio: jsonString,
  TopupGroupRatio: jsonString,
  UserUsableGroups: jsonString,
  GroupGroupRatio: jsonString,
  AutoGroups: z.string().superRefine((value, ctx) => {
    const result = validateJsonString(value, {
      predicate: (parsed) =>
        Array.isArray(parsed) &&
        parsed.every((item) => typeof item === 'string'),
      predicateMessage: 'Expected a JSON array of group identifiers',
    })
    if (!result.valid) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: result.message || 'Invalid JSON array',
      })
    }
  }),
  DefaultUseAutoGroup: z.boolean(),
  GroupSpecialUsableGroup: jsonString,
  AmountDiscount: jsonObjectString,
})

export type CustomerTierSettingsDefaults = z.infer<typeof customerTierSchema>

function formatDefaults(defaults: CustomerTierSettingsDefaults) {
  return {
    ...defaults,
    GroupRatio: formatJsonForTextarea(defaults.GroupRatio),
    TopupGroupRatio: formatJsonForTextarea(defaults.TopupGroupRatio),
    UserUsableGroups: formatJsonForTextarea(defaults.UserUsableGroups),
    GroupGroupRatio: formatJsonForTextarea(defaults.GroupGroupRatio),
    AutoGroups: formatJsonForTextarea(defaults.AutoGroups),
    GroupSpecialUsableGroup: formatJsonForTextarea(
      defaults.GroupSpecialUsableGroup
    ),
    AmountDiscount: formatJsonForTextarea(defaults.AmountDiscount),
  }
}

export function CustomerTierSettingsCard({
  defaultValues,
}: {
  defaultValues: CustomerTierSettingsDefaults
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const form = useForm<CustomerTierSettingsDefaults>({
    resolver: zodResolver(customerTierSchema),
    mode: 'onChange',
    defaultValues: formatDefaults(defaultValues),
  })

  useEffect(() => {
    form.reset(formatDefaults(defaultValues))
  }, [defaultValues, form])

  const update = useMutation({
    mutationFn: updateSystemOptionsBulk,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['system-options'] })
      queryClient.invalidateQueries({ queryKey: ['pricing'] })
      queryClient.invalidateQueries({ queryKey: ['user-group-options'] })
      toast.success(t('Setting updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to update setting'))
    },
  })

  const handleSave = async (values: CustomerTierSettingsDefaults) => {
    const apiKeyMap: Record<string, string> = {
      GroupSpecialUsableGroup: 'group_ratio_setting.group_special_usable_group',
      AmountDiscount: 'payment_setting.amount_discount',
    }
    await update.mutateAsync({
      options: Object.entries(values).map(([key, value]) => ({
        key: apiKeyMap[key] || key,
        value: typeof value === 'string' ? normalizeJsonString(value) : value,
      })),
    })
  }

  return (
    <SettingsSection
      title={t('Customer tiers & discounts')}
      description={t(
        'Manage customer tiers, token groups, exclusive pricing, and recharge discounts'
      )}
    >
      <GroupRatioForm
        form={form}
        onSave={handleSave}
        isSaving={update.isPending}
      />
    </SettingsSection>
  )
}
