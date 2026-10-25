import type { TFunction } from 'i18next'
import type { SettingsSectionNavItem } from '@/features/system-settings/components/settings-section-tabs'

// eflow: 管理员工作区「领域制」核心配置。
// 把后端那 7 个 system-settings「区」（general/models/auth/request-limits/
// integrations/content/maintenance）彻底打散，~40 个设置分区按**产品领域**重新归
// 9 类。每个分区仍是独立路由（/system-settings/<area>/<section>），所以可以自由
// 跨区归类。本表同时驱动：① 侧栏每个领域的「设置」入口；② 进入设置后页内的「领域 tab」
// （跨区，替代原来按后端区的 tab）。titleKey 复用各分区已有的 i18n 键（都有中文翻译）。

export type DomainSection = { area: string; section: string; titleKey: string }
export type AdminSettingsDomain = { id: string; sections: DomainSection[] }

export const ADMIN_SETTINGS_DOMAINS: AdminSettingsDomain[] = [
  {
    id: 'customer-benefits',
    sections: [
      {
        area: 'models',
        section: 'customer-tiers',
        titleKey: 'Customer tiers & discounts',
      },
    ],
  },
  {
    id: 'gateway',
    sections: [
      {
        area: 'general',
        section: 'channel-affinity',
        titleKey: 'Channel Affinity',
      },
      { area: 'models', section: 'ratio', titleKey: 'Pricing Ratios' },
      {
        area: 'models',
        section: 'global',
        titleKey: 'Global Model Configuration',
      },
      { area: 'models', section: 'gemini', titleKey: 'Gemini' },
      { area: 'models', section: 'claude', titleKey: 'Claude' },
      { area: 'models', section: 'grok', titleKey: 'Grok' },
    ],
  },
  {
    id: 'customer-auth',
    sections: [
      { area: 'auth', section: 'basic-auth', titleKey: 'Basic Authentication' },
      {
        area: 'auth',
        section: 'oauth-integrations',
        titleKey: 'OAuth Integrations',
      },
      { area: 'auth', section: 'passkey', titleKey: 'Passkey Authentication' },
      { area: 'auth', section: 'bot-protection', titleKey: 'Bot Protection' },
      { area: 'auth', section: 'custom-oauth', titleKey: 'Custom OAuth' },
    ],
  },
  {
    id: 'request-security',
    sections: [
      {
        area: 'request-limits',
        section: 'rate-limit',
        titleKey: 'Rate Limiting',
      },
      {
        area: 'request-limits',
        section: 'sensitive-words',
        titleKey: 'Sensitive Words',
      },
      {
        area: 'request-limits',
        section: 'ssrf-protection',
        titleKey: 'SSRF Protection',
      },
    ],
  },
  {
    id: 'finance',
    sections: [
      { area: 'integrations', section: 'payment', titleKey: 'Payment Gateway' },
      { area: 'general', section: 'quota', titleKey: 'Quota Settings' },
      { area: 'general', section: 'pricing', titleKey: 'Pricing & Display' },
      { area: 'general', section: 'checkin', titleKey: 'Check-in Rewards' },
    ],
  },
  {
    id: 'usage-refunds',
    sections: [
      {
        area: 'maintenance',
        section: 'usage-refunds',
        titleKey: 'Batch balance return',
      },
    ],
  },
  {
    id: 'ticket',
    sections: [
      { area: 'integrations', section: 'ticket', titleKey: 'Ticket System' },
    ],
  },
  {
    id: 'site',
    sections: [
      { area: 'content', section: 'announcements', titleKey: 'Announcements' },
      { area: 'content', section: 'faq', titleKey: 'FAQ' },
      { area: 'content', section: 'api-info', titleKey: 'API Addresses' },
      {
        area: 'general',
        section: 'system-info',
        titleKey: 'System Information',
      },
      {
        area: 'maintenance',
        section: 'header-navigation',
        titleKey: 'Header navigation',
      },
      {
        area: 'maintenance',
        section: 'sidebar-modules',
        titleKey: 'Sidebar modules',
      },
      { area: 'content', section: 'chat', titleKey: 'Chat Presets' },
      { area: 'content', section: 'drawing', titleKey: 'Drawing' },
      { area: 'content', section: 'uptime-kuma', titleKey: 'Uptime Kuma' },
    ],
  },
  {
    id: 'notify',
    sections: [
      { area: 'integrations', section: 'email', titleKey: 'SMTP Email' },
      {
        area: 'integrations',
        section: 'email-templates',
        titleKey: 'Email Templates',
      },
      { area: 'integrations', section: 'worker', titleKey: 'Worker Proxy' },
      {
        area: 'integrations',
        section: 'ionet',
        titleKey: 'io.net Deployments',
      },
      {
        area: 'integrations',
        section: 'monitoring',
        titleKey: 'Monitoring & Alerts',
      },
      {
        area: 'integrations',
        section: 'group-monitoring',
        titleKey: 'Group Monitoring',
      },
    ],
  },
  {
    id: 'system',
    sections: [
      {
        area: 'maintenance',
        section: 'update-checker',
        titleKey: 'System maintenance',
      },
      { area: 'maintenance', section: 'notice', titleKey: 'System Notice' },
      { area: 'maintenance', section: 'logs', titleKey: 'Log Maintenance' },
      { area: 'maintenance', section: 'performance', titleKey: 'Performance' },
      { area: 'content', section: 'dashboard', titleKey: 'Data Dashboard' },
      { area: 'general', section: 'behavior', titleKey: 'System Behavior' },
      {
        area: 'integrations',
        section: 'web-group-access',
        titleKey: 'Web Group Access',
      },
    ],
  },
]

function sectionUrl(s: DomainSection): string {
  return `/system-settings/${s.area}/${s.section}`
}

/** 找出某个 (area, section) 属于哪个领域 */
export function findDomainOf(
  area: string,
  section: string
): AdminSettingsDomain | undefined {
  return ADMIN_SETTINGS_DOMAINS.find((d) =>
    d.sections.some((s) => s.area === area && s.section === section)
  )
}

/**
 * 进入某分区后，页内「领域 tab」用的导航项 —— 返回该分区所属**整个领域**的分区
 * （跨后端区）。id 用 `area/section` 保证跨区唯一；activeSection 侧传同样格式。
 */
export function getDomainTabItems(
  t: TFunction,
  area: string,
  section: string
): SettingsSectionNavItem[] {
  const d = findDomainOf(area, section)
  if (!d) return []
  return d.sections.map((s) => ({
    id: `${s.area}/${s.section}`,
    title: t(s.titleKey),
    url: sectionUrl(s),
  }))
}

/** 侧栏某领域「设置」入口的落地 URL（该领域第一个分区） */
export function getDomainFirstUrl(domainId: string): string {
  const d = ADMIN_SETTINGS_DOMAINS.find((x) => x.id === domainId)
  return d ? sectionUrl(d.sections[0]) : '/system-settings'
}

/** 侧栏某领域「设置」入口的 activeUrls（该领域全部分区 URL，用于高亮） */
export function getDomainSectionUrls(domainId: string): string[] {
  const d = ADMIN_SETTINGS_DOMAINS.find((x) => x.id === domainId)
  return d ? d.sections.map(sectionUrl) : []
}
