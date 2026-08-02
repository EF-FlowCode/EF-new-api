/**
 * Hook for checking admin privileges
 */
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'

/**
 * Check if current user has admin privileges
 */
export function useIsAdmin(): boolean {
  const { user } = useAuthStore((state) => state.auth)
  return (user?.role ?? 0) >= ROLE.ADMIN
}

// eflow: 客服 (role>=5) 也算 staff — 用于"读多于写"的 UI 闸（如点击用户名弹画像）。
// 真正改写权限仍走 useIsAdmin / 后端 AdminAuth。
export function useIsStaff(): boolean {
  const { user } = useAuthStore((state) => state.auth)
  return (user?.role ?? 0) >= ROLE.SUPPORT
}
