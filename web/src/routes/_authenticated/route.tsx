import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { getSelf } from '@/lib/api'
import { AuthenticatedLayout } from '@/components/layout'
import {
  getWorkspaceByPath,
  WORKSPACE_IDS,
} from '@/components/layout/lib/workspace-registry'
import { ROLE } from '@/lib/roles'

// 内存中的验证标记，避免同一会话中重复验证
let sessionVerified = false

// eflow: 统一壳改造——公开页（模型广场 / 分组监控）也在带侧栏的统一壳内
// 渲染，访客无需登录即可浏览。这些路径前缀放行守卫；其余路径仍强制登录。
const PUBLIC_PREFIXES = ['/pricing', '/group-monitoring', '/leaderboard']

function isPublicPath(pathname: string): boolean {
  return PUBLIC_PREFIXES.some(
    (p) => pathname === p || pathname.startsWith(p + '/')
  )
}

export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ location }) => {
    const { auth } = useAuthStore.getState()
    const pub = isPublicPath(location.pathname)

    // 本地没有用户信息：访客在公开页放行，其余跳登录页
    if (!auth.user) {
      if (pub) return
      throw redirect({
        to: '/sign-in',
        search: { redirect: location.href },
      })
    }

    // 本地有用户信息，但需要验证 session 是否有效（每个会话只验证一次）
    if (!sessionVerified) {
      const res = await getSelf().catch(() => null)
      if (res?.success && res.data) {
        // 验证成功，更新用户信息（可能有变化）
        auth.setUser(res.data)
        sessionVerified = true
      } else {
        // session 失效：清缓存。公开页降级为访客浏览，其余跳登录页
        auth.reset()
        if (pub) return
        throw redirect({
          to: '/sign-in',
          search: { redirect: location.href },
        })
      }
    }

    // eflow: admin 工作区（运营页 channels/users/... + system-settings/*）纵深守卫。
    // 后端 AdminAuth/RootAuth 才是真正边界；这里是前端纵深防御 + 干净的 403 体验：
    // 非 staff（role < SUPPORT）即便手敲 /channels、/users 等 admin URL，也一律踢到
    // /403，不再加载 admin 页面外壳。system-settings/* 另有更严的 SUPER_ADMIN 守卫。
    const role = useAuthStore.getState().auth.user?.role ?? 0
    const inAdminWorkspace =
      getWorkspaceByPath(location.pathname).id ===
      WORKSPACE_IDS.SYSTEM_SETTINGS
    if (inAdminWorkspace && role < ROLE.SUPPORT) {
      throw redirect({ to: '/403' })
    }
  },
  component: AuthenticatedLayout,
})
