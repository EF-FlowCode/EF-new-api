import { getCookie } from '@/lib/cookies'
import { cn } from '@/lib/utils'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider } from '@/context/search-provider'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { AnimatedOutlet } from '@/components/page-transition'
import { SkipToMain } from '@/components/skip-to-main'
import { WorkspaceProvider } from '../context/workspace-context'
import { AppSidebar } from './app-sidebar'
import { ProjectAttribution } from './footer'

type AuthenticatedLayoutProps = {
  children?: React.ReactNode
}

export function AuthenticatedLayout(props: AuthenticatedLayoutProps) {
  const defaultOpen = getCookie('sidebar_state') !== 'false'

  return (
    <LayoutProvider>
      <SearchProvider>
        <WorkspaceProvider>
          <SidebarProvider defaultOpen={defaultOpen}>
            <SkipToMain />
            <AppSidebar />
            <SidebarInset
              className={cn(
                '@container/content',
                'h-svh',
                'peer-data-[variant=inset]:h-[calc(100svh-(var(--spacing)*4))]'
              )}
            >
              <div className='flex min-h-0 flex-1 flex-col'>
                {props.children ?? <AnimatedOutlet />}
              </div>
              {/* eflow: AGPLv3 §7/§13 常驻署名条——每页可见的 footer 归属位置。
                  上游 Footer 组件在单壳重构后成了未挂载的死代码，这里重新挂上
                  ProjectAttribution（QuantumNous 原项目链接 + 署名原文 + §13 修改版源码链接）。 */}
              <footer className='shrink-0 border-t px-4 py-1.5'>
                <ProjectAttribution currentYear={new Date().getFullYear()} />
              </footer>
            </SidebarInset>
          </SidebarProvider>
        </WorkspaceProvider>
      </SearchProvider>
    </LayoutProvider>
  )
}
