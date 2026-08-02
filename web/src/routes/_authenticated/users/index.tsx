import z from 'zod'
import { createFileRoute, redirect } from '@tanstack/react-router'
import { useAuthStore } from '@/stores/auth-store'
import { ROLE } from '@/lib/roles'
import { Users } from '@/features/users'

const usersSearchSchema = z.object({
  page: z.number().optional().catch(1),
  pageSize: z.number().optional().catch(10),
  // eflow: filter 是旧的单一关键字搜索，已被下面三个字段级搜索框取代；
  // 保留 schema 项只为兼容旧书签 URL，不再有 UI 入口。
  filter: z.string().optional().catch(''),
  // eflow: 三个独立搜索框 → 三个独立 URL query，后端各自 AND 过滤、跨页正确。
  search_id: z.string().optional().catch(''),
  search_name: z.string().optional().catch(''),
  search_email: z.string().optional().catch(''),
  status: z
    .array(z.enum(['1', '2']))
    .optional()
    .catch([]),
  role: z
    .array(z.enum(['1', '5', '10', '100']))
    .optional()
    .catch([]),
  // eflow: group 改用单选 facet, columnFilter type='array' 写出 string[]（长度 0 或 1）。
  // schema 接 array<string>，老的 ?group=foo 形式由 .catch([]) 兜底成 []，UI 显示无选择。
  group: z.array(z.string()).optional().catch([]),
})

export const Route = createFileRoute('/_authenticated/users/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()

    // eflow: 客服 (role>=5) 也允许进，但页面内对 mutating 入口做隐藏
    if (!auth.user || auth.user.role < ROLE.SUPPORT) {
      throw redirect({
        to: '/403',
      })
    }
  },
  validateSearch: usersSearchSchema,
  component: Users,
})
