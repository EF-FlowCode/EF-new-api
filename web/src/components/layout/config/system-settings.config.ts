import { type TFunction } from 'i18next'
import {
  Box,
  BadgePercent,
  Gift,
  Headset,
  Lock,
  Megaphone,
  Plug,
  Radio,
  Receipt,
  RotateCcw,
  Settings,
  ShieldAlert,
  Sliders,
  Ticket,
  Users,
  Wrench,
} from 'lucide-react'
import { ROLE } from '@/lib/roles'
import { type NavGroup } from '../types'
import { getDomainFirstUrl, getDomainSectionUrls } from './admin-domains'

/**
 * Admin workspace sidebar configuration
 *
 * eflow: 原本是"系统设置" workspace（只含 7 大类配置）。这次重组把原主侧栏 Admin 组的
 * 8 个数据/运营路由（Channels/Users/Tickets/...）也搬进同一个 workspace，按 7 大业务
 * 对象重新分组（模型/用户与权限/财务/工单/外观/通知集成/系统），不再"运营 vs 配置"二分。
 *
 * URL 路径保持原样：admin 路由 (/channels, /users, ...) + system-settings 子路径
 * (/system-settings/<category>/<section>) 全部不变；只是侧栏入口位置与文案换了。
 *
 * 角色门槛：每个 item 用 minRole 声明（types.ts）。app-sidebar 按当前用户 role
 * 统一过滤，原本写死 group.id === 'admin' 的逻辑撤掉。
 */
export const WORKSPACE_SYSTEM_SETTINGS_ID = 'system-settings'

export function getSystemSettingsNavGroups(t: TFunction): NavGroup[] {
  return [
    {
      id: 'gateway',
      title: t('Gateway'),
      items: [
        {
          title: t('Channels'),
          url: '/channels',
          icon: Radio,
          minRole: ROLE.ADMIN,
        },
        {
          title: t('Model Metadata'),
          url: '/models/metadata',
          icon: Box,
          minRole: ROLE.ADMIN,
        },
        {
          // eflow: 领域制——网关设置(渠道亲和/倍率/模型行为/厂商参数)跨后端 general+models 区，
          // 入口落到第一个分区,进入后页内是「领域 tab」。详见 admin-domains.ts。
          title: t('Gateway Settings'),
          url: getDomainFirstUrl('gateway'),
          activeUrls: getDomainSectionUrls('gateway'),
          icon: Sliders,
          minRole: ROLE.SUPER_ADMIN,
        },
      ],
    },
    {
      id: 'customer',
      title: t('Customers'),
      items: [
        {
          title: t('Users'),
          url: '/users',
          icon: Users,
          minRole: ROLE.SUPPORT,
        },
        {
          title: t('Customer tiers & discounts'),
          url: getDomainFirstUrl('customer-benefits'),
          activeUrls: getDomainSectionUrls('customer-benefits'),
          icon: BadgePercent,
          minRole: ROLE.SUPER_ADMIN,
        },
        {
          title: t('Invitation Codes'),
          url: '/invitation-codes',
          icon: Gift,
          minRole: ROLE.ADMIN,
        },
        {
          title: t('Redemption Codes'),
          url: '/redemption-codes',
          icon: Ticket,
          minRole: ROLE.ADMIN,
        },
        {
          title: t('Risk Center'),
          url: '/risk-center',
          icon: ShieldAlert,
          minRole: ROLE.ADMIN,
        },
        {
          title: t('Account & Security'),
          icon: Lock,
          minRole: ROLE.SUPER_ADMIN,
          items: [
            {
              title: t('Login & Authentication'),
              url: getDomainFirstUrl('customer-auth'),
              activeUrls: getDomainSectionUrls('customer-auth'),
            },
            {
              title: t('Request Security'),
              url: getDomainFirstUrl('request-security'),
              activeUrls: getDomainSectionUrls('request-security'),
            },
          ],
        },
      ],
    },
    {
      id: 'finance',
      title: t('Finance'),
      items: [
        {
          // eflow: 领域制——财务(支付网关/配额规则/定价与显示/签到奖励)跨 integrations+general 区。
          title: t('Billing Settings'),
          url: getDomainFirstUrl('finance'),
          activeUrls: getDomainSectionUrls('finance'),
          icon: Receipt,
          minRole: ROLE.SUPER_ADMIN,
        },
        {
          title: t('Batch balance return'),
          url: getDomainFirstUrl('usage-refunds'),
          activeUrls: getDomainSectionUrls('usage-refunds'),
          icon: RotateCcw,
          minRole: ROLE.SUPER_ADMIN,
        },
      ],
    },
    {
      id: 'tickets',
      title: t('Tickets'),
      items: [
        {
          title: t('Ticket Admin'),
          url: '/admin-tickets',
          icon: Headset,
          minRole: ROLE.SUPPORT,
        },
        {
          title: t('Ticket Settings'),
          url: getDomainFirstUrl('ticket'),
          activeUrls: getDomainSectionUrls('ticket'),
          icon: Wrench,
          minRole: ROLE.SUPER_ADMIN,
        },
      ],
    },
    {
      id: 'site',
      title: t('Site & Content'),
      items: [
        {
          // eflow: 领域制——站点内容(公告/FAQ/API地址/系统信息/导航布局/侧栏模块/对话/绘图/
          // Uptime Kuma)跨 content+general+maintenance 区。
          title: t('Site Settings'),
          url: getDomainFirstUrl('site'),
          activeUrls: getDomainSectionUrls('site'),
          icon: Megaphone,
          minRole: ROLE.SUPER_ADMIN,
        },
      ],
    },
    {
      id: 'notify',
      title: t('Notifications & Integrations'),
      items: [
        {
          // eflow: 领域制——通知集成(SMTP/邮件模板/Worker/io.net/监控告警/分组监控)同 integrations 区。
          title: t('Integration Settings'),
          url: getDomainFirstUrl('notify'),
          activeUrls: getDomainSectionUrls('notify'),
          icon: Plug,
          minRole: ROLE.SUPER_ADMIN,
        },
      ],
    },
    {
      id: 'system',
      title: t('System & Ops'),
      items: [
        {
          // eflow: 领域制——系统运维(维护/更新/系统公告/日志/性能/数据导出/系统行为)跨
          // maintenance+content+general 区。
          title: t('System Settings'),
          url: getDomainFirstUrl('system'),
          activeUrls: getDomainSectionUrls('system'),
          icon: Settings,
          minRole: ROLE.SUPER_ADMIN,
        },
      ],
    },
  ]
}
