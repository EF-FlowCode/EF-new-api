import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import type { CustomOAuthBinding } from '@/lib/oauth'
import { securityProofHeaders } from '@/lib/secure-verification'
import type { BuiltinBindingType } from './lib/builtin-binding-types'
import type {
  User,
  GetUsersParams,
  GetUsersResponse,
  SearchUsersParams,
  UserFormData,
  ManageUserAction,
  ManageUserQuotaPayload,
  ApiResponse,
} from './types'

// ============================================================================
// User Management APIs
// ============================================================================

/**
 * Get paginated users list
 */
// Administrative user changes carry a single-use, user-bound proof when the
// backend requires one (status/role changes, deletion, password or email
// resets, security resets, binding removal).
function withProof(proofToken?: string) {
  return {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    headers: securityProofHeaders(proofToken),
  }
}

export async function getUsers(
  params: GetUsersParams = {}
): Promise<GetUsersResponse> {
  const {
    p = 1,
    page_size = 10,
    sort_by,
    sort_order,
    role,
    status,
    group,
  } = params
  const qs = new URLSearchParams({
    p: String(p),
    page_size: String(page_size),
  })
  if (sort_by) qs.set('sort_by', sort_by)
  if (sort_order) qs.set('sort_order', sort_order)
  // eflow: role / status 多选 → 重复 query key (?role=1&role=5)，后端 QueryArray 收集
  if (role && role.length > 0) {
    for (const r of role) qs.append('role', String(r))
  }
  if (status && status.length > 0) {
    for (const s of status) qs.append('status', String(s))
  }
  // eflow: group 单值，空串不挂 query
  if (group) qs.set('group', group)
  const res = await api.get(`/api/user/?${qs.toString()}`)
  return res.data
}

/**
 * Search users by keyword or group
 */
export async function searchUsers(
  params: SearchUsersParams
): Promise<GetUsersResponse> {
  const {
    keyword = '',
    search_id = '',
    search_username = '',
    search_email = '',
    group = '',
    p = 1,
    page_size = 10,
    sort_by,
    sort_order,
    role,
    status,
  } = params
  const qs = new URLSearchParams({
    p: String(p),
    page_size: String(page_size),
  })
  // eflow: 三个字段级搜索框——非空才挂 query，后端各自 AND 过滤；keyword 仍兼容老逻辑
  if (keyword) qs.set('keyword', keyword)
  if (search_id) qs.set('search_id', search_id)
  if (search_username) qs.set('search_username', search_username)
  if (search_email) qs.set('search_email', search_email)
  if (group) qs.set('group', group)
  if (sort_by) qs.set('sort_by', sort_by)
  if (sort_order) qs.set('sort_order', sort_order)
  // eflow: role / status 多选 → 重复 query key
  if (role && role.length > 0) {
    for (const r of role) qs.append('role', String(r))
  }
  if (status && status.length > 0) {
    for (const s of status) qs.append('status', String(s))
  }
  const res = await api.get(`/api/user/search?${qs.toString()}`)
  return res.data
}

/**
 * Get single user by ID
 */
export async function getUser(id: number): Promise<ApiResponse<User>> {
  const res = await api.get(`/api/user/${id}`)
  return res.data
}

/**
 * Create a new user
 */
export async function createUser(
  data: UserFormData,
  proofToken?: string
): Promise<ApiResponse<User>> {
  const res = await api.post('/api/user/', data, withProof(proofToken))
  return res.data
}

/**
 * Update an existing user
 */
export async function updateUser(
  data: UserFormData & { id: number },
  proofToken?: string
): Promise<ApiResponse<Partial<User>>> {
  const res = await api.put('/api/user/', data, withProof(proofToken))
  return res.data
}

/**
 * Delete a single user (hard delete)
 */
export async function deleteUser(
  id: number,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/${id}/`, withProof(proofToken))
  return res.data
}

/**
 * Manage user (promote, demote, enable, disable, delete)
 */
export async function manageUser(
  id: number,
  action: ManageUserAction,
  proofToken?: string
): Promise<ApiResponse<Partial<User>>> {
  const res = await api.post(
    '/api/user/manage',
    { id, action },
    withProof(proofToken)
  )
  return res.data
}

// eflow: 把任意用户切到指定 role（含 5=客服）。后端 set_role 用 value 字段携带目标角色。
export async function setUserRole(
  id: number,
  role: number,
  proofToken?: string
): Promise<ApiResponse<Partial<User>>> {
  const res = await api.post(
    '/api/user/manage',
    {
      id,
      action: 'set_role',
      value: role,
    },
    withProof(proofToken)
  )
  return res.data
}

/**
 * Adjust user quota atomically (add/subtract/override)
 */
export async function adjustUserQuota(
  payload: ManageUserQuotaPayload
): Promise<ApiResponse<Partial<User>>> {
  const res = await api.post(
    '/api/user/manage',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Reset user's Passkey registration
 */
export async function resetUserPasskey(
  id: number,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete(
    `/api/user/${id}/reset_passkey`,
    withProof(proofToken)
  )
  return res.data
}

/**
 * Reset user's Two-Factor Authentication setup
 */
export async function resetUserTwoFA(
  id: number,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/${id}/2fa`, withProof(proofToken))
  return res.data
}

/** Get token routing groups used by channels and model availability. */
export async function getGroups(): Promise<ApiResponse<string[]>> {
  const res = await api.get('/api/group/')
  return res.data
}

/** Get customer tiers available for users. */
export async function getUserGroupOptions(): Promise<ApiResponse<string[]>> {
  const res = await api.get('/api/user/group_options')
  return res.data
}

// eflow: 用户列表筛选下拉只展示 users.group 实际值；客户等级编辑选项则使用
// getUserGroupOptions()，还会包含尚未分配给用户的新等级。
// 端点 /api/user/distinct_groups 挂在 staffReadRoute（TicketStaffAuth），客服可调。
export async function getUserDistinctGroups(): Promise<ApiResponse<string[]>> {
  const res = await api.get('/api/user/distinct_groups')
  return res.data
}

// ============================================================================
// Admin Binding Management APIs
// ============================================================================

/**
 * Get user's custom OAuth bindings (admin)
 */
export async function getUserOAuthBindings(
  userId: number
): Promise<ApiResponse<CustomOAuthBinding[]>> {
  const res = await api.get(
    `/api/user/${userId}/oauth/bindings`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Clear a user's built-in binding (admin)
 */
export async function adminClearUserBinding(
  userId: number,
  bindingType: BuiltinBindingType,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete(
    `/api/user/${userId}/bindings/${bindingType}`,
    withProof(proofToken)
  )
  return res.data
}

/**
 * Unbind custom OAuth for a user (admin)
 */
export async function adminUnbindCustomOAuth(
  userId: number,
  providerId: number,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete(
    `/api/user/${userId}/oauth/bindings/${providerId}`,
    withProof(proofToken)
  )
  return res.data
}
