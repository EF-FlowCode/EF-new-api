import { useMutation, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'
import { updateSystemOption } from '../api'
import type { UpdateOptionRequest } from '../types'

// Configuration keys that require status refresh
const STATUS_RELATED_KEYS = [
  'HeaderNavModules',
  'SidebarModulesAdmin',
  'GroupMonitoringURL',
  'Notice',
  'LogConsumeEnabled',
  'QuotaPerUnit',
  'USDExchangeRate',
  'DisplayInCurrencyEnabled',
  'DisplayTokenStatEnabled',
  'InvitationCodeUserGenerateEnabled',
  'group_monitoring_setting.monitoring_groups',
  'console_setting.api_info_enabled',
  'console_setting.announcements_enabled',
  'console_setting.faq_enabled',
  'console_setting.uptime_kuma_enabled',
  'general_setting.quota_display_type',
  'general_setting.custom_currency_symbol',
  'general_setting.custom_currency_exchange_rate',
]

const PRICING_RELATED_KEYS = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
  'GroupRatio',
  'GroupGroupRatio',
  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
]

export function useUpdateOption() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: (request: UpdateOptionRequest) => updateSystemOption(request),
    onSuccess: (_data, variables) => {
      // Always refresh system-options
      queryClient.invalidateQueries({ queryKey: ['system-options'] })

      // If updating frontend-display-related config, also refresh status
      if (STATUS_RELATED_KEYS.includes(variables.key)) {
        queryClient.invalidateQueries({ queryKey: ['status'] })
      }

      if (PRICING_RELATED_KEYS.includes(variables.key)) {
        queryClient.invalidateQueries({ queryKey: ['pricing'] })
      }

      toast.success(i18next.t('Setting updated successfully'))
    },
    onError: (error: Error) => {
      toast.error(error.message || i18next.t('Failed to update setting'))
    },
  })
}
