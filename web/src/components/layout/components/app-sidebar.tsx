import { useEffect, useMemo, useState } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { Megaphone, Search, Shield } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useLayout } from '@/context/layout-provider'
import { useNotifications } from '@/hooks/use-notifications'
import { useSidebarConfig } from '@/hooks/use-sidebar-config'
import { useSidebarData } from '@/hooks/use-sidebar-data'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarInput,
  SidebarRail,
  useSidebar,
} from '@/components/ui/sidebar'
import { NotificationPopover } from '@/components/notification-popover'
import {
  getNavGroupsForPath,
  isInWorkspace,
  WORKSPACE_IDS,
} from '../lib/workspace-registry'
import type { NavGroup as NavGroupType, NavItem } from '../types'
import { NavGroup } from './nav-group'
import { WorkspaceSwitcher } from './workspace-switcher'

/**
 * Application sidebar component
 * Fetches corresponding navigation menu from workspace registry based on current path
 * Dynamically filters navigation items based on backend SidebarModulesAdmin configuration
 *
 * Automatically matches workspace configuration for current path through workspace registry system
 * Adding new workspaces only requires registration in workspace-registry.ts
 */
function filterNavGroupsByQuery(
  groups: NavGroupType[],
  query: string
): NavGroupType[] {
  const q = query.trim().toLowerCase()
  if (!q) return groups

  const matches = (text: string | undefined) =>
    !!text && text.toLowerCase().includes(q)

  const filtered: NavGroupType[] = []
  for (const group of groups) {
    if (matches(group.title)) {
      filtered.push(group)
      continue
    }
    const items: NavItem[] = []
    for (const item of group.items) {
      if ('items' in item && item.items) {
        const subs = item.items.filter((s) => matches(s.title))
        if (subs.length > 0) {
          items.push({ ...item, items: subs })
          continue
        }
      }
      if (matches(item.title)) items.push(item)
    }
    if (items.length > 0) filtered.push({ ...group, items })
  }
  return filtered
}

function SidebarSearch({
  value,
  onChange,
}: {
  value: string
  onChange: (v: string) => void
}) {
  const { t } = useTranslation()
  const { state } = useSidebar()
  if (state === 'collapsed') return null
  return (
    <div className='relative px-2 py-1.5'>
      <Search className='text-muted-foreground pointer-events-none absolute start-4 top-1/2 size-4 -translate-y-1/2' />
      <SidebarInput
        type='search'
        placeholder={t('Search menu...')}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className='h-8 ps-8 text-xs'
      />
    </div>
  )
}

/**
 * eflow: 主侧栏底部"管理员"快捷入口——仅 ROLE.SUPPORT+ 登录用户可见，且只在主
 * workspace 显示（已经在 admin workspace 里就不再显示这个按钮，避免噪音）。
 * 折叠态显图标 only；展开态显图标 + 文案。
 */
function AdminEntryButton() {
  const { t } = useTranslation()
  const { state } = useSidebar()
  const collapsed = state === 'collapsed'
  return (
    <Button
      asChild
      variant='outline'
      size={collapsed ? 'icon' : 'sm'}
      className={collapsed ? 'w-full' : 'w-full justify-start gap-2'}
      title={t('Admin')}
    >
      <Link to='/channels'>
        <Shield className='size-4 shrink-0' />
        {!collapsed && <span>{t('Admin')}</span>}
      </Link>
    </Button>
  )
}

// 每次页面加载最多自动展开一次（侧栏在路由切换时会重新挂载）。
let announcementsAutoOpened = false

/**
 * eflow: 侧栏左下角"公告"入口，样式与"管理员"入口一致，点开为上游样式的气泡面板。
 * 有未读的通知或公告时，桌面端自动展开一次并全部标记已读；手机端侧栏收起时只显示未读数。
 */
function AnnouncementEntry() {
  const { t } = useTranslation()
  const { state, isMobile } = useSidebar()
  const collapsed = state === 'collapsed' && !isMobile
  const notifications = useNotifications()
  const { loading, unreadCount, unreadNoticeCount, openPopover } = notifications

  useEffect(() => {
    if (announcementsAutoOpened || isMobile || loading || unreadCount === 0) {
      return
    }
    announcementsAutoOpened = true
    openPopover(unreadNoticeCount > 0 ? 'notice' : 'announcements', {
      markAll: true,
    })
  }, [isMobile, loading, unreadCount, unreadNoticeCount, openPopover])

  const unreadLabel = unreadCount > 99 ? '99+' : String(unreadCount)

  return (
    <NotificationPopover
      open={notifications.popoverOpen}
      onOpenChange={notifications.setPopoverOpen}
      activeTab={notifications.activeTab}
      onTabChange={notifications.setActiveTab}
      notice={notifications.notice}
      announcements={notifications.announcements}
      loading={notifications.loading}
      side={isMobile ? 'top' : 'right'}
      align='end'
    >
      <Button
        variant='outline'
        size={collapsed ? 'icon' : 'sm'}
        className={cn(
          'relative',
          collapsed ? 'w-full' : 'w-full justify-start gap-2'
        )}
        title={t('Announcements')}
        aria-label={t('Announcements')}
      >
        <Megaphone className='size-4 shrink-0' />
        {!collapsed && <span>{t('Announcements')}</span>}
        {unreadCount > 0 &&
          (collapsed ? (
            <span className='bg-destructive absolute top-1 right-1 size-2 rounded-full' />
          ) : (
            <Badge
              variant='destructive'
              className='ms-auto h-5 min-w-5 justify-center rounded-full px-1 text-[10px] font-semibold tabular-nums'
            >
              {unreadLabel}
            </Badge>
          ))}
      </Button>
    </NotificationPopover>
  )
}

export function AppSidebar() {
  const { t } = useTranslation()
  const { collapsible, variant } = useLayout()
  const { pathname } = useLocation()
  const userRole = useAuthStore((state) => state.auth.user?.role)
  const isGuest = useAuthStore((state) => !state.auth.user)
  const sidebarData = useSidebarData()
  const [query, setQuery] = useState('')

  // Get navigation group configuration corresponding to current path from workspace registry
  const allNavGroups = getNavGroupsForPath(pathname, t) || sidebarData.navGroups

  // Filter sidebar navigation items based on backend configuration
  const configFilteredNavGroups = useSidebarConfig(allNavGroups)

  // eflow: 角色过滤改 item-level——每个 nav item 用 minRole 声明自己的最低门槛
  //   - 未设置 minRole：登录用户都可见
  //   - 设置了 minRole：仅 userRole >= minRole 才可见
  //   - 嵌套的 collapsible item 也按 minRole 过滤其子项
  // 原来 SUPPORT_ADMIN_URLS 白名单 + group.id === 'admin' 硬编码的过滤逻辑撤掉，
  // 现在 Admin workspace 的每个 nav item 自己声明 minRole（详见
  // components/layout/config/system-settings.config.ts）。
  const roleFilteredNavGroups = useMemo(() => {
    const role = userRole ?? 0
    const allowItem = (item: NavItem) =>
      item.minRole === undefined || role >= item.minRole
    return configFilteredNavGroups
      .map((group) => {
        const items = group.items
          .map((item) => {
            if ('items' in item && item.items) {
              const subs = item.items.filter((s) => allowItem(s))
              if (subs.length === 0) return null
              return { ...item, items: subs }
            }
            return allowItem(item) ? item : null
          })
          .filter((it): it is NavItem => it !== null)
        return { ...group, items }
      })
      .filter((group) => group.items.length > 0)
  }, [configFilteredNavGroups, userRole])

  // eflow: 访客（未登录）只在侧栏看到公开页入口（模型广场 / 分组监控 / 关于）
  // external:true 的外链 (例: 文档站) 也放行——这类链接不受本站鉴权约束
  const GUEST_VISIBLE_URLS = useMemo(
    () => new Set<string>(['/pricing', '/group-monitoring']),
    []
  )
  const guestFilteredNavGroups = useMemo(() => {
    if (!isGuest) return roleFilteredNavGroups
    return roleFilteredNavGroups
      .map((group) => ({
        ...group,
        items: group.items.filter(
          (item) =>
            'url' in item &&
            (GUEST_VISIBLE_URLS.has(item.url ?? '') ||
              (item as NavItem & { external?: boolean }).external === true)
        ),
      }))
      .filter((group) => group.items.length > 0)
  }, [isGuest, roleFilteredNavGroups, GUEST_VISIBLE_URLS])

  const currentNavGroups = useMemo(
    () => filterNavGroupsByQuery(guestFilteredNavGroups, query),
    [guestFilteredNavGroups, query]
  )

  // eflow: 主侧栏 Footer 入口
  //   - 所有人：「公告」入口（原右上角铃铛挪到这里）
  //   - 访客：Sign in 按钮（原状）
  //   - 登录 + SUPPORT+ + 当前在主 workspace：「管理员」快捷按钮 + 菜单搜索
  //   - 登录 + 已在 admin workspace：仅菜单搜索（避免按钮自指）
  //   - 登录 + 普通用户：仅菜单搜索（看不到 admin 入口）
  const role = userRole ?? 0
  const inAdminWorkspace = isInWorkspace(
    pathname,
    WORKSPACE_IDS.SYSTEM_SETTINGS
  )
  const showAdminEntry = !isGuest && role >= ROLE.SUPPORT && !inAdminWorkspace

  return (
    <Sidebar collapsible={collapsible} variant={variant}>
      <SidebarHeader>
        <WorkspaceSwitcher workspaces={sidebarData.workspaces} />
      </SidebarHeader>
      <SidebarContent>
        {currentNavGroups.map((props) => {
          const key = props.id || props.title
          return <NavGroup key={key} {...props} />
        })}
        {query && currentNavGroups.length === 0 && (
          <div className='text-muted-foreground px-4 py-6 text-center text-xs'>
            {t('No matches')}
          </div>
        )}
      </SidebarContent>
      <SidebarFooter>
        <div className='flex flex-col gap-2 p-2'>
          <AnnouncementEntry />
          {isGuest ? (
            <Button asChild className='w-full'>
              <Link to='/sign-in'>{t('Sign in')}</Link>
            </Button>
          ) : (
            showAdminEntry && <AdminEntryButton />
          )}
        </div>
        {!isGuest && <SidebarSearch value={query} onChange={setQuery} />}
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
