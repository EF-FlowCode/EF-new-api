import type { IntegrationSettings } from '../types'
import { createSectionRegistry } from '../utils/section-registry'
import { EmailSettingsSection } from './email-settings-section'
import { EmailTemplateSettingsSection } from './email-template-section'
import { GroupMonitoringSettingsSection } from './group-monitoring-section'
import { IoNetDeploymentSettingsSection } from './ionet-deployment-settings-section'
import { MonitoringSettingsSection } from './monitoring-settings-section'
import { PaymentSettingsSection } from './payment-settings-section'
// eflow: 工单设置面板统一用 xifeng 的 ticket-section.tsx（功能更全：附件配置 +
// 客服分配等）。我之前 31d3bc9f 引入的 ticket-settings-section.tsx 已删，避免
// 重复 import 造成 TicketSettingsSection 名字冲突 + 重复注册两个 ticket section。
import { TicketSettingsSection } from './ticket-section'
import { WorkerSettingsSection } from './worker-settings-section'

const INTEGRATIONS_SECTIONS = [
  {
    id: 'payment',
    titleKey: 'Payment Gateway',
    descriptionKey: 'Configure payment gateway integrations',
    build: (settings: IntegrationSettings) => (
      <PaymentSettingsSection
        defaultValues={{
          PayAddress: settings.PayAddress,
          EpayId: settings.EpayId,
          EpayKey: settings.EpayKey,
          Price: settings.Price,
          MinTopUp: settings.MinTopUp,
          CustomCallbackAddress: settings.CustomCallbackAddress,
          PayMethods: settings.PayMethods,
          AmountOptions: settings['payment_setting.amount_options'],
          AmountDiscount: settings['payment_setting.amount_discount'],
          StripeApiSecret: settings.StripeApiSecret,
          StripeWebhookSecret: settings.StripeWebhookSecret,
          StripePriceId: settings.StripePriceId,
          StripeUnitPrice: settings.StripeUnitPrice,
          StripeMinTopUp: settings.StripeMinTopUp,
          StripePromotionCodesEnabled: settings.StripePromotionCodesEnabled,
          CreemApiKey: settings.CreemApiKey,
          CreemWebhookSecret: settings.CreemWebhookSecret,
          CreemTestMode: settings.CreemTestMode,
          CreemProducts: settings.CreemProducts,
        }}
        complianceConfirmed={settings['payment_setting.compliance_confirmed']}
        complianceConfirmedAt={
          settings['payment_setting.compliance_confirmed_at']
        }
        complianceTermsVersion={
          settings['payment_setting.compliance_terms_version']
        }
        waffoDefaultValues={{
          WaffoEnabled: settings.WaffoEnabled ?? false,
          WaffoApiKey: settings.WaffoApiKey ?? '',
          WaffoPrivateKey: settings.WaffoPrivateKey ?? '',
          WaffoPublicCert: settings.WaffoPublicCert ?? '',
          WaffoSandboxPublicCert: settings.WaffoSandboxPublicCert ?? '',
          WaffoSandboxApiKey: settings.WaffoSandboxApiKey ?? '',
          WaffoSandboxPrivateKey: settings.WaffoSandboxPrivateKey ?? '',
          WaffoSandbox: settings.WaffoSandbox ?? false,
          WaffoMerchantId: settings.WaffoMerchantId ?? '',
          WaffoCurrency: settings.WaffoCurrency ?? 'USD',
          WaffoUnitPrice: settings.WaffoUnitPrice ?? 1,
          WaffoMinTopUp: settings.WaffoMinTopUp ?? 1,
          WaffoNotifyUrl: settings.WaffoNotifyUrl ?? '',
          WaffoReturnUrl: settings.WaffoReturnUrl ?? '',
          WaffoPayMethods: settings.WaffoPayMethods ?? '[]',
        }}
        waffoPancakeDefaultValues={{
          WaffoPancakeEnabled: settings.WaffoPancakeEnabled ?? false,
          WaffoPancakeSandbox: settings.WaffoPancakeSandbox ?? false,
          WaffoPancakeMerchantID: settings.WaffoPancakeMerchantID ?? '',
          WaffoPancakePrivateKey: settings.WaffoPancakePrivateKey ?? '',
          WaffoPancakeWebhookPublicKey:
            settings.WaffoPancakeWebhookPublicKey ?? '',
          WaffoPancakeWebhookTestKey: settings.WaffoPancakeWebhookTestKey ?? '',
          WaffoPancakeStoreID: settings.WaffoPancakeStoreID ?? '',
          WaffoPancakeProductID: settings.WaffoPancakeProductID ?? '',
          WaffoPancakeReturnURL: settings.WaffoPancakeReturnURL ?? '',
          WaffoPancakeCurrency: settings.WaffoPancakeCurrency ?? 'USD',
          WaffoPancakeUnitPrice: settings.WaffoPancakeUnitPrice ?? 1,
          WaffoPancakeMinTopUp: settings.WaffoPancakeMinTopUp ?? 1,
        }}
      />
    ),
  },
  {
    id: 'email',
    titleKey: 'SMTP Email',
    descriptionKey: 'Configure SMTP email settings',
    build: (settings: IntegrationSettings) => (
      <EmailSettingsSection
        defaultValues={{
          SMTPServer: settings.SMTPServer,
          SMTPPort: settings.SMTPPort,
          SMTPAccount: settings.SMTPAccount,
          SMTPFrom: settings.SMTPFrom,
          SMTPToken: settings.SMTPToken,
          SMTPArchiveBcc: settings.SMTPArchiveBcc,
          SMTPSSLEnabled: settings.SMTPSSLEnabled,
          SMTPForceAuthLogin: settings.SMTPForceAuthLogin,
        }}
      />
    ),
  },
  {
    id: 'worker',
    titleKey: 'Worker Proxy',
    descriptionKey: 'Configure worker service settings',
    build: (settings: IntegrationSettings) => (
      <WorkerSettingsSection
        defaultValues={{
          WorkerUrl: settings.WorkerUrl,
          WorkerValidKey: settings.WorkerValidKey,
          WorkerAllowHttpImageRequestEnabled:
            settings.WorkerAllowHttpImageRequestEnabled,
        }}
      />
    ),
  },
  {
    id: 'ionet',
    titleKey: 'io.net Deployments',
    descriptionKey: 'Configure IoNet model deployment settings',
    build: (settings: IntegrationSettings) => (
      <IoNetDeploymentSettingsSection
        defaultValues={{
          enabled: settings['model_deployment.ionet.enabled'],
          apiKey: settings['model_deployment.ionet.api_key'],
        }}
      />
    ),
  },
  {
    id: 'monitoring',
    titleKey: 'Monitoring & Alerts',
    descriptionKey: 'Configure channel monitoring and automation',
    build: (settings: IntegrationSettings) => (
      <MonitoringSettingsSection
        defaultValues={{
          ChannelDisableThreshold: settings.ChannelDisableThreshold,
          QuotaRemindThreshold: settings.QuotaRemindThreshold,
          AutomaticDisableChannelEnabled:
            settings.AutomaticDisableChannelEnabled,
          AutomaticEnableChannelEnabled: settings.AutomaticEnableChannelEnabled,
          AutomaticDisableKeywords: settings.AutomaticDisableKeywords,
          AutomaticDisableStatusCodes: settings.AutomaticDisableStatusCodes,
          AutomaticRetryStatusCodes: settings.AutomaticRetryStatusCodes,
          'monitor_setting.auto_test_channel_enabled':
            settings['monitor_setting.auto_test_channel_enabled'],
          'monitor_setting.auto_test_channel_minutes':
            settings['monitor_setting.auto_test_channel_minutes'],
        }}
      />
    ),
  },
  {
    id: 'group-monitoring',
    titleKey: 'Group Monitoring',
    descriptionKey: 'Tune the group availability dashboard',
    build: (settings: IntegrationSettings) => (
      <GroupMonitoringSettingsSection
        defaultValues={{
          'group_monitoring_setting.monitoring_groups':
            settings['group_monitoring_setting.monitoring_groups'],
          'group_monitoring_setting.group_display_order':
            settings['group_monitoring_setting.group_display_order'],
          'group_monitoring_setting.availability_period_minutes':
            settings['group_monitoring_setting.availability_period_minutes'],
          'group_monitoring_setting.cache_hit_period_minutes':
            settings['group_monitoring_setting.cache_hit_period_minutes'],
          'group_monitoring_setting.aggregation_interval_minutes':
            settings['group_monitoring_setting.aggregation_interval_minutes'],
          'group_monitoring_setting.availability_exclude_models':
            settings['group_monitoring_setting.availability_exclude_models'],
          'group_monitoring_setting.cache_hit_exclude_models':
            settings['group_monitoring_setting.cache_hit_exclude_models'],
          'group_monitoring_setting.availability_exclude_keywords':
            settings['group_monitoring_setting.availability_exclude_keywords'],
          'group_monitoring_setting.availability_exclude_status_codes':
            settings[
              'group_monitoring_setting.availability_exclude_status_codes'
            ],
          'group_monitoring_setting.cache_tokens_separate_groups':
            settings['group_monitoring_setting.cache_tokens_separate_groups'],
        }}
      />
    ),
  },
  {
    id: 'email-templates',
    titleKey: 'Email Templates',
    descriptionKey: 'Customize subject and HTML body of system emails',
    build: () => <EmailTemplateSettingsSection />,
  },
  {
    id: 'ticket',
    titleKey: 'Ticket System',
    descriptionKey: 'Notification, attachments and ticket assignment',
    build: (settings: IntegrationSettings) => (
      <TicketSettingsSection
        defaultValues={{
          TicketNotifyEnabled: settings.TicketNotifyEnabled,
          TicketAdminEmail: settings.TicketAdminEmail,
          TicketAttachmentEnabled: settings.TicketAttachmentEnabled,
          TicketAttachmentMaxSize: settings.TicketAttachmentMaxSize,
          TicketAttachmentMaxCount: settings.TicketAttachmentMaxCount,
          TicketAttachmentAllowedExts: settings.TicketAttachmentAllowedExts,
          TicketAttachmentLocalPath: settings.TicketAttachmentLocalPath,
          TicketAttachmentSignedURLTTL: settings.TicketAttachmentSignedURLTTL,
          TicketAssignConfig: settings.TicketAssignConfig,
          TicketReplyAttachmentEnabled: settings.TicketReplyAttachmentEnabled,
          TicketReplyAttachmentMaxBytes: settings.TicketReplyAttachmentMaxBytes,
          TicketInvoiceMinTopupMoney: settings.TicketInvoiceMinTopupMoney,
        }}
      />
    ),
  },
] as const

export type IntegrationSectionId = (typeof INTEGRATIONS_SECTIONS)[number]['id']

const integrationsRegistry = createSectionRegistry<
  IntegrationSectionId,
  IntegrationSettings
>({
  sections: INTEGRATIONS_SECTIONS,
  defaultSection: 'payment',
  basePath: '/system-settings/integrations',
  urlStyle: 'path',
})

export const INTEGRATIONS_SECTION_IDS = integrationsRegistry.sectionIds
export const INTEGRATIONS_DEFAULT_SECTION = integrationsRegistry.defaultSection
export const getIntegrationsSectionNavItems =
  integrationsRegistry.getSectionNavItems
export const getIntegrationsSectionContent =
  integrationsRegistry.getSectionContent
