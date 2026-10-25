import { useMutation, useQueryClient } from '@tanstack/react-query'
import i18next from 'i18next'
import { toast } from 'sonner'
import {
  OptionSavedNotPublishedError,
  updateSystemOption,
  updateSystemOptionsBulk,
} from '../api'
import type { UpdateOptionRequest } from '../types'

// Configuration keys that feed /api/status and so require a status refresh.
const STATUS_RELATED_KEYS = new Set([
  'HeaderNavModules',
  'SidebarModulesAdmin',
  'GroupMonitoringURL',
  'Notice',
  'LogConsumeEnabled',
  'QuotaPerUnit',
  'USDExchangeRate',
  'Price',
  'StripeUnitPrice',
  'DisplayInCurrencyEnabled',
  'InvitationCodeEnabled',
  'InvitationCodeOAuthRequired',
  'InvitationCodeUserGenerateEnabled',
  'SystemName',
  'Logo',
  'Footer',
  'ServerAddress',
  'Chats',
  'AffiliateEnabled',
  'DrawingEnabled',
  'TaskEnabled',
  'DataExportEnabled',
  'DataExportDefaultTime',
  'DefaultCollapseSidebar',
  'DefaultUseAutoGroup',
  'MjNotifyEnabled',
  'DemoSiteEnabled',
  'SelfUseModeEnabled',
  'RegisterEnabled',
  'PasswordLoginEnabled',
  'PasswordRegisterEnabled',
  'EmailVerificationEnabled',
  'TurnstileCheckEnabled',
  'TurnstileSiteKey',
  'GitHubOAuthEnabled',
  'GitHubClientId',
  'LinuxDOOAuthEnabled',
  'LinuxDOClientId',
  'LinuxDOMinimumTrustLevel',
  'TelegramOAuthEnabled',
  'TelegramBotName',
  'TelegramClientId',
  'WeChatAuthEnabled',
  'WeChatAccountQRCodeImageURL',
  'TicketAttachmentEnabled',
  'TicketAttachmentMaxSize',
  'TicketAttachmentMaxCount',
  'TicketAttachmentAllowedExts',
])

const STATUS_RELATED_PREFIXES = [
  'console_setting.',
  'general_setting.',
  'group_monitoring_setting.',
  'legal.',
  'checkin_setting.',
  'oidc.',
  'passkey.',
  'discord.',
]

export function isStatusRelatedKey(key: string) {
  return (
    STATUS_RELATED_KEYS.has(key) ||
    STATUS_RELATED_PREFIXES.some((prefix) => key.startsWith(prefix))
  )
}

const PRICING_RELATED_KEYS = [
  'ModelPricing',
  'BatchInputRatio',
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
  'billing_setting.task_expressions',
  'PricingGroupEndpointOverrides',
  'ModelContextWindows',
]

export function getOptionInvalidationQueryKeys(options: UpdateOptionRequest[]) {
  const keys = [['system-options']]
  if (options.some((option) => isStatusRelatedKey(option.key))) {
    keys.push(['status'])
  }
  if (options.some((option) => PRICING_RELATED_KEYS.includes(option.key))) {
    keys.push(['pricing'])
  }
  return keys
}

export function submitOptionsBulk(
  options: UpdateOptionRequest[],
  submit: typeof updateSystemOptionsBulk = updateSystemOptionsBulk
) {
  return submit({ options })
}

function useOptionMutation<T>(
  mutationFn: (variables: T) => Promise<unknown>,
  toOptions: (variables: T) => UpdateOptionRequest[]
) {
  const queryClient = useQueryClient()
  const invalidate = (variables: T) => {
    for (const queryKey of getOptionInvalidationQueryKeys(
      toOptions(variables)
    )) {
      queryClient.invalidateQueries({ queryKey })
    }
  }

  return useMutation({
    mutationFn,
    onSuccess: (_data, variables) => {
      invalidate(variables)
      toast.success(i18next.t('Setting updated successfully'))
    },
    onError: (error: Error, variables) => {
      // The database now holds the new values but this process rejected them;
      // retrying the sync would reuse the same values, so ask for a corrected
      // save instead of promising it applies later. Refetch so the form stops
      // showing its local edit as if nothing was written.
      if (error instanceof OptionSavedNotPublishedError) {
        invalidate(variables)
        toast.error(
          i18next.t(
            'Setting was written to the database but could not be applied; check the system logs and save a corrected value'
          )
        )
        return
      }
      toast.error(error.message || i18next.t('Failed to update setting'))
    },
  })
}

export function useUpdateOption() {
  return useOptionMutation(
    (request: UpdateOptionRequest) => updateSystemOption(request),
    (request) => [request]
  )
}

export function useUpdateOptions() {
  return useOptionMutation(
    (options: UpdateOptionRequest[]) => submitOptionsBulk(options),
    (options) => options
  )
}
