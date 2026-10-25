import { z } from 'zod'

// ============================================================================
// User Schema & Types
// ============================================================================

/** User status: 1 = enabled, 2 = disabled, 3+ = other states */
export const userStatusSchema = z.number()
export type UserStatus = z.infer<typeof userStatusSchema>

/** User role: 1 = common user, 10 = admin, 100 = root */
export const userRoleSchema = z.number()
export type UserRole = z.infer<typeof userRoleSchema>

export const userSchema = z.object({
  id: z.number(),
  username: z.string(),
  display_name: z.string(),
  password: z.string().optional(),
  github_id: z.string().optional(),
  oidc_id: z.string().optional(),
  wechat_id: z.string().optional(),
  telegram_id: z.string().optional(),
  email: z.string().optional(),
  quota: z.number(),
  used_quota: z.number(),
  request_count: z.number(),
  group: z.string(),
  additional_customer_tiers: z.array(z.string()).default([]),
  aff_code: z.string().optional(),
  aff_count: z.number().optional(),
  aff_quota: z.number().optional(),
  aff_history_quota: z.number().optional(),
  inviter_id: z.number().optional(),
  linux_do_id: z.string().optional(),
  status: userStatusSchema,
  role: userRoleSchema,
  created_at: z.number().optional(),
  updated_at: z.number().optional(),
  last_login_at: z.number().optional(),
  DeletedAt: z.any().nullable().optional(),
  remark: z.string().optional(),
})
export type User = z.infer<typeof userSchema>

export const userListSchema = z.array(userSchema)

// ============================================================================
// API Request/Response Types
// ============================================================================

/** Generic API response */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  data?: T
}

export interface GetUsersParams {
  p?: number
  page_size?: number
  // 后端 model.NewUserSortOptions 白名单：
  // id / username / quota / status / role / created_at
  sort_by?: string
  sort_order?: 'asc' | 'desc'
  // eflow: 后端按 role IN (?) 过滤，多选转重复 query key (?role=1&role=5)
  role?: number[]
  // eflow: 后端按 status IN (?) 过滤，多选转重复 query key
  status?: number[]
  // eflow: 后端按 group = ? 精确过滤（单值；users.group 是单字段不是多选）
  group?: string
}

export interface GetUsersResponse {
  success: boolean
  message?: string
  data?: {
    items: User[]
    total: number
    page: number
    page_size: number
  }
}

export interface SearchUsersParams {
  keyword?: string
  // eflow: 三个独立搜索框——各自只过滤自己的字段（后端 AND 组合，不跨字段 OR）
  // search_id: 精确匹配 id；search_username: 覆盖 username + display_name(姓名)；
  // search_email: 模糊匹配 email
  search_id?: string
  search_username?: string
  search_email?: string
  group?: string
  p?: number
  page_size?: number
  sort_by?: string
  sort_order?: 'asc' | 'desc'
  // eflow: 后端按 role IN (?) 过滤，多选转重复 query key
  role?: number[]
  // eflow: 后端按 status IN (?) 过滤
  status?: number[]
}

export interface UserFormData {
  username: string
  display_name: string
  email?: string
  password?: string
  role?: number // Only used when creating user
  quota?: number // Only used when updating user
  group?: string
  additional_customer_tiers?: string[]
  remark?: string // Only used when updating user
}

export type ManageUserAction =
  | 'promote'
  | 'demote'
  | 'set_role'
  | 'enable'
  | 'disable'
  | 'delete'
  | 'add_quota'

export type QuotaAdjustMode = 'add' | 'subtract' | 'override'

export interface ManageUserQuotaPayload {
  id: number
  action: 'add_quota'
  mode: QuotaAdjustMode
  value: number
}

// ============================================================================
// Dialog Types
// ============================================================================

export type UsersDialogType = 'create' | 'update' | 'delete'
