import * as React from 'react'
import { Link, useNavigate, useLocation } from '@tanstack/react-router'
import { ChevronsUpDown, Home } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { useStatus } from '@/hooks/use-status'
import { useSystemConfig } from '@/hooks/use-system-config'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { useWorkspace } from '../context/workspace-context'
import { getWorkspaceByPath, WORKSPACE_IDS } from '../lib/workspace-registry'
import { type Workspace } from '../types'

type WorkspaceSwitcherProps = {
  workspaces: Workspace[]
  defaultName?: string
}

/**
 * Workspace switcher component
 * Allows users to switch between different workspaces
 * - Regular users can only see the default workspace
 * - Super administrators can see the system settings workspace
 *
 * eflow: 原本接受 defaultVersion 作为版本号 fallback；4f44c36d4 改为 plan 永远
 * 置空字符串后该参数不再使用，本次清理一并移除。
 */
export function WorkspaceSwitcher({
  workspaces,
  defaultName = 'New API',
}: WorkspaceSwitcherProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { pathname } = useLocation()
  const { isMobile } = useSidebar()
  const { status } = useStatus()
  const { logo } = useSystemConfig()
  // eflow: 原本只让 SUPER_ADMIN 看到 admin workspace 入口。重组后 admin workspace
  // 里有 SUPPORT 也能用的项（Users / Ticket Admin），所以门槛降到 ROLE.SUPPORT；
  // 真正"只有 super admin 能用"的项由各 nav item 的 minRole 控制（详见
  // app-sidebar.tsx 的过滤逻辑）。
  const canAccessAdmin = useAuthStore(
    (state) => (state.auth.user?.role ?? 0) >= ROLE.SUPPORT
  )
  const { activeWorkspace, setActiveWorkspace } = useWorkspace()

  // Handle workspace list:
  // 1. Populate first workspace with system info
  // 2. Filter based on user permissions (non-staff users cannot see admin workspace)
  const availableWorkspaces = React.useMemo(
    () =>
      workspaces
        .map((workspace, index) =>
          index === 0
            ? {
                ...workspace,
                name: status?.system_name || defaultName,
                // eflow: 不显示版本号——空串 + 下方 span 条件渲染
                plan: '',
              }
            : workspace
        )
        .filter(
          (workspace) =>
            canAccessAdmin || workspace.id !== WORKSPACE_IDS.SYSTEM_SETTINGS
        ),
    // eflow: status?.version / defaultVersion / t 已无引用（plan 永远空串），从 deps 移除
    [workspaces, status?.system_name, defaultName, canAccessAdmin]
  )

  // Initialize and synchronize active workspace
  // Detect from URL first, then sync from activeWorkspace
  React.useEffect(() => {
    // Detect which workspace should be active from workspace registry
    const detectedWorkspace = getWorkspaceByPath(pathname)

    if (detectedWorkspace.id === WORKSPACE_IDS.SYSTEM_SETTINGS) {
      // Currently in system settings route, should activate System Settings workspace
      const systemSettingsWorkspace = availableWorkspaces.find(
        (w) => w.id === WORKSPACE_IDS.SYSTEM_SETTINGS
      )
      if (systemSettingsWorkspace) {
        setActiveWorkspace(systemSettingsWorkspace)
      }
    } else {
      // Currently in main workspace route, should activate main workspace
      const mainWorkspace =
        availableWorkspaces.find((w) => w.id === WORKSPACE_IDS.DEFAULT) ||
        availableWorkspaces[0]
      if (mainWorkspace) {
        setActiveWorkspace(mainWorkspace)
      }
    }
  }, [pathname, availableWorkspaces, setActiveWorkspace])

  const handleWorkspaceChange = (workspace: Workspace) => {
    // Only navigate, let useEffect synchronize workspace state based on new pathname
    // This avoids race conditions and context loss issues
    if (workspace.id === WORKSPACE_IDS.SYSTEM_SETTINGS) {
      // eflow: 按角色选落地页——Admin(>=10) 跳 /channels 看运营数据，
      // 客服/财务(5-9) 跳 /admin-tickets 直接进工单后台。
      // 原来一律跳 /channels，导致 role<10 的用户切 workspace 就 403。
      const userRole = useAuthStore.getState().auth.user?.role ?? 0
      if (userRole >= ROLE.ADMIN) {
        navigate({ to: '/channels' })
      } else {
        navigate({ to: '/admin-tickets' })
      }
    } else {
      navigate({ to: '/dashboard' })
    }
  }

  if (!activeWorkspace) {
    return null
  }

  const canSwitchWorkspace = availableWorkspaces.length > 1
  const workspaceButtonContent = (
    <>
      {activeWorkspace.id === WORKSPACE_IDS.SYSTEM_SETTINGS ? (
        <div className='bg-sidebar-primary text-sidebar-primary-foreground flex aspect-square size-8 items-center justify-center rounded-lg'>
          <activeWorkspace.logo className='size-4' />
        </div>
      ) : (
        <div className='flex aspect-square size-8 items-center justify-center overflow-hidden rounded-lg'>
          <img
            src={logo}
            alt={t('Logo')}
            className='size-full rounded-lg object-cover'
          />
        </div>
      )}
      <div className='grid flex-1 text-start text-sm leading-tight group-data-[collapsible=icon]:hidden'>
        <span className='truncate font-semibold'>{activeWorkspace.name}</span>
        {activeWorkspace.plan && (
          <span className='truncate text-xs'>{activeWorkspace.plan}</span>
        )}
      </div>
      {canSwitchWorkspace && (
        <ChevronsUpDown className='ms-auto group-data-[collapsible=icon]:hidden' />
      )}
    </>
  )

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        {canSwitchWorkspace ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                size='lg'
                className='data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground'
              >
                {workspaceButtonContent}
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              className='w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg'
              align='start'
              side={isMobile ? 'bottom' : 'right'}
              sideOffset={4}
            >
              {/* eflow: 下拉顶部加"主页"入口跳 portal,与 sidebar 底部 admin 入口呼应,
                  避免 workspace 下拉成为唯一回主页的方式 */}
              <DropdownMenuItem
                onClick={() => navigate({ to: '/' })}
                className='gap-2 p-2'
              >
                <div className='flex size-6 items-center justify-center rounded-sm border'>
                  <Home className='size-4 shrink-0' />
                </div>
                {t('Home')}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuLabel className='text-muted-foreground text-xs'>
                {t('Workspaces')}
              </DropdownMenuLabel>
              {availableWorkspaces.map((workspace, index) => (
                <DropdownMenuItem
                  key={workspace.id}
                  onClick={() => handleWorkspaceChange(workspace)}
                  className='gap-2 p-2'
                >
                  {index === 0 ? (
                    <div className='flex size-6 items-center justify-center overflow-hidden rounded-sm border'>
                      <img
                        src={logo}
                        alt='Logo'
                        className='size-full object-cover'
                      />
                    </div>
                  ) : (
                    <div className='flex size-6 items-center justify-center rounded-sm border'>
                      <workspace.logo className='size-4 shrink-0' />
                    </div>
                  )}
                  {workspace.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : (
          // eflow: 单一 workspace (普通用户) 时,整个按钮变成跳主页的 Link,
          // 不再是 cursor-default 的死按钮,给非 admin 用户一个明确的回主页入口
          <SidebarMenuButton asChild size='lg'>
            <Link to='/'>{workspaceButtonContent}</Link>
          </SidebarMenuButton>
        )}
      </SidebarMenuItem>
    </SidebarMenu>
  )
}
