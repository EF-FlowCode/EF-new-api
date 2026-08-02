import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { useNotifications } from '@/hooks/use-notifications'
import { Button } from '@/components/ui/button'
import { LanguageSwitcher } from '@/components/language-switcher'
import { NotificationButton } from '@/components/notification-button'
import { NotificationDialog } from '@/components/notification-dialog'
import { ProfileDropdown } from '@/components/profile-dropdown'
import { Search } from '@/components/search'
import { ThemeSwitch } from '@/components/theme-switch'
import { Header } from './header'

/**
 * 统一壳的顶部工具条。
 *
 * eflow: 统一 UI 改造后，顶栏不再承载页面导航链接（首页/控制台/分组监控…）。
 * 导航全部交给左侧边栏；顶栏退化为纯工具条：搜索 / 通知 / 语言 / 主题 /
 * 头像（未登录时显示"登录"按钮）。
 * 旧的 ConfigDrawer 入口已撤——它的 Palette 图标和 ThemeSwitch 冲撞，
 * 且 sidebar variant / direction 这类设置普通用户不会动。组件本身保留，
 * 错误页等仍可独立挂载。
 */
type AppHeaderProps = {
  /** 左侧自定义内容（默认空，仅留侧栏开关）。 */
  leftContent?: React.ReactNode
  /** 右侧自定义内容，提供后覆盖默认右侧区。 */
  rightContent?: React.ReactNode
  /** 是否显示搜索框 @default true */
  showSearch?: boolean
  /** 是否显示通知按钮 @default true */
  showNotifications?: boolean
  /** 是否显示头像 / 登录按钮 @default true */
  showProfileDropdown?: boolean
}

export function AppHeader({
  leftContent,
  rightContent,
  showSearch = true,
  showNotifications = true,
  showProfileDropdown = true,
}: AppHeaderProps) {
  const { t } = useTranslation()
  const user = useAuthStore((state) => state.auth.user)
  const notifications = useNotifications()

  return (
    <>
      <Header>
        {leftContent}
        {rightContent ?? (
          <div className='ms-auto flex items-center space-x-4'>
            {showSearch && <Search />}
            {showNotifications && (
              <NotificationButton
                unreadCount={notifications.unreadCount}
                onClick={() => notifications.openDialog()}
              />
            )}
            <LanguageSwitcher />
            <ThemeSwitch />
            {showProfileDropdown &&
              (user ? (
                <ProfileDropdown />
              ) : (
                <Button size='sm' className='h-8 rounded-lg px-3.5' asChild>
                  <Link to='/sign-in'>{t('Sign in')}</Link>
                </Button>
              ))}
          </div>
        )}
      </Header>

      {/* Notification Dialog */}
      {showNotifications && (
        <NotificationDialog
          open={notifications.dialogOpen}
          onOpenChange={notifications.setDialogOpen}
          activeTab={notifications.activeTab}
          onTabChange={notifications.setActiveTab}
          notice={notifications.notice}
          announcements={notifications.announcements}
          loading={notifications.loading}
          onCloseToday={notifications.closeToday}
        />
      )}
    </>
  )
}
