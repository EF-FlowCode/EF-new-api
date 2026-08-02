import { type LinkProps } from '@tanstack/react-router'

/**
 * Workspace type
 * Used for top switcher to display different workspaces
 */
export type Workspace = {
  id: string
  name: string
  logo: React.ElementType
  plan: string
}

/**
 * Base navigation item type
 */
type BaseNavItem = {
  title: string
  badge?: string
  icon?: React.ElementType
  activeUrls?: (LinkProps['to'] | (string & {}))[]
  configUrls?: (LinkProps['to'] | (string & {}))[]
  // eflow: 外链项（如文档站）— 渲染为 <a target=_blank>，不走 TanStack <Link>
  external?: boolean
  // eflow: 该 item 的最低 ROLE 门槛（如 ROLE.SUPPORT / ROLE.ADMIN / ROLE.SUPER_ADMIN）。
  // app-sidebar 会按当前用户 role 过滤。未设置表示对登录用户都可见。
  // 原本 admin 组按 group.id === 'admin' 硬编码过滤，现在散到各 item，更通用。
  minRole?: number
}

/**
 * Navigation link type - single link item
 */
export type NavLink = BaseNavItem & {
  url: LinkProps['to'] | (string & {})
  items?: never
  type?: never
}

/**
 * Navigation collapsible type - collapsible navigation with sub-items
 */
export type NavCollapsible = BaseNavItem & {
  items: (BaseNavItem & { url: LinkProps['to'] | (string & {}) })[]
  url?: never
  type?: never
}

/**
 * Dynamic chat presets type - dynamically loaded chat preset list from API
 */
export type NavChatPresets = BaseNavItem & {
  type: 'chat-presets'
  url?: never
  items?: never
}

/**
 * Navigation item union type
 */
export type NavItem = NavCollapsible | NavLink | NavChatPresets

/**
 * Navigation group type - a group of navigation items in sidebar
 */
export type NavGroup = {
  id?: string
  title: string
  items: NavItem[]
}

/**
 * Sidebar data type
 */
export type SidebarData = {
  workspaces: Workspace[]
  navGroups: NavGroup[]
}

/**
 * Top navigation link type
 */
export type TopNavLink = {
  title: string
  href: string
  isActive?: boolean
  disabled?: boolean
  external?: boolean
}
