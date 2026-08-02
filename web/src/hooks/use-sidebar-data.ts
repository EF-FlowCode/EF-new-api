import {
  LayoutDashboard,
  Activity,
  BookOpen,
  Gauge,
  Key,
  FileText,
  Wallet,
  User,
  Command,
  FlaskConical,
  LifeBuoy,
  MessageSquare,
  ListTodo,
  Mail,
  Receipt,
  Shield,
  Store,
  Trophy,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useIsStaff } from '@/hooks/use-admin'
import { useStatus } from '@/hooks/use-status'
import { WORKSPACE_IDS } from '@/components/layout/lib/workspace-registry'
import { type NavItem, type SidebarData } from '@/components/layout/types'

export function useSidebarData(): SidebarData {
  const { t } = useTranslation()
  const { status } = useStatus()
  const isStaff = useIsStaff()

  // eflow: 从后端 /api/status 读 docs_link (admin 在系统设置里改它)
  // 之前的顶栏 Docs 入口随 "统一 UI 壳" 公开壳删除被弃了，这里把它挂回侧栏
  // 资源组的「模型广场」下面
  const docsLink =
    typeof (status as Record<string, unknown> | null)?.docs_link === 'string'
      ? ((status as Record<string, unknown>).docs_link as string).trim()
      : ''

  const resourceItems: NavItem[] = [
    {
      title: t('Model Square'),
      url: '/pricing',
      icon: Store,
    },
    {
      title: t('Leaderboard'),
      url: '/leaderboard',
      icon: Trophy,
    },
  ]
  if (docsLink) {
    resourceItems.push({
      title: t('Docs'),
      url: docsLink,
      icon: BookOpen,
      external: true,
    })
  }

  return {
    // eflow: workspaces 数组里同时挂载主站 + 管理员 workspace，让左上角
    // WorkspaceSwitcher 在管理员 workspace 里也能切回主站（原版只挂载 DEFAULT，
    // 导致进了 system-settings 后 canSwitchWorkspace=false，回不来）。
    workspaces: [
      {
        id: WORKSPACE_IDS.DEFAULT,
        name: '', // Dynamically fetches system name
        logo: Command,
        plan: '', // Dynamically fetches system version
      },
      {
        id: WORKSPACE_IDS.SYSTEM_SETTINGS,
        name: t('Admin'),
        logo: Shield,
        plan: '',
      },
    ],
    navGroups: [
      {
        // eflow: 公开资源组——访客也能浏览，置顶方便访问
        id: 'resource',
        title: t('Resources'),
        items: resourceItems,
      },
      {
        id: 'chat',
        title: t('Chat'),
        items: [
          {
            title: t('Playground'),
            url: '/playground',
            icon: FlaskConical,
          },
          {
            title: t('Chat'),
            icon: MessageSquare,
            type: 'chat-presets',
          },
        ],
      },
      {
        id: 'general',
        title: t('General'),
        items: [
          {
            title: t('Overview'),
            url: '/dashboard/overview',
            icon: Activity,
          },
          {
            title: t('Model Analytics'),
            url: '/dashboard/models',
            icon: LayoutDashboard,
          },
          {
            title: t('API Keys'),
            url: '/keys',
            icon: Key,
          },
          {
            title: t('Usage Logs'),
            url: '/usage-logs/common',
            icon: FileText,
          },
          {
            title: t('Task Logs'),
            url: '/usage-logs/task',
            activeUrls: ['/usage-logs/drawing'],
            configUrls: ['/usage-logs/drawing', '/usage-logs/task'],
            icon: ListTodo,
          },
          ...(status?.group_monitoring_available === true
            ? [
                {
                  title: t('Group Monitoring'),
                  url: '/group-monitoring',
                  icon: Gauge,
                },
              ]
            : []),
        ],
      },
      {
        id: 'personal',
        title: t('Personal'),
        items: [
          {
            title: t('Wallet'),
            url: '/wallet',
            icon: Wallet,
          },
          {
            title: t('Topup History'),
            url: '/topup-history',
            icon: Receipt,
          },
          {
            title: t('Support tickets'),
            url: '/tickets',
            icon: LifeBuoy,
          },
          ...(status?.invitation_code_user_generate_enabled === true || isStaff
            ? [
                {
                  title: t('My invitation codes'),
                  url: '/my-invitation-codes',
                  icon: Mail,
                },
              ]
            : []),
          {
            title: t('Profile'),
            url: '/profile',
            icon: User,
          },
        ],
      },
      // eflow: 原 admin 组（Channels/Models/Users/Codes/Tickets/Risk/
      // System Settings）整组撤掉，全部迁入 Admin workspace 的 7 大领域
      // （详见 components/layout/config/system-settings.config.ts）。主侧栏只剩
      // "普通用户视角"——站长登录默认也是这个视角，要管理时再从底部入口或左上
      // WorkspaceSwitcher 切到 Admin workspace。
    ],
  }
}
