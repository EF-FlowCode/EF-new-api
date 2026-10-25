import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'

export type AuditCategory = 'login' | 'security' | 'operation' | 'access_token'

export interface AuditOperation {
  action: string
  params?: Record<string, unknown>
}

export interface AuditOther {
  op?: AuditOperation
  admin_info?: {
    admin_id?: number
    admin_username?: string
    admin_role?: number
    auth_method?: string
  }
  audit_info?: {
    method?: string
    route?: string
    status?: number
    success?: boolean
    params?: Record<string, string>
  }
  root_info?: Record<string, unknown>
  login_method?: string
  user_agent?: string
}

export interface AuditLog {
  id: number
  event_id: string
  user_id: number
  username: string
  actor_role: number
  created_at: number
  category: AuditCategory
  action: string
  token_ref: string
  auth_method?: string
  ip: string
  user_agent: string
  method: string
  route: string
  status: number
  success: boolean
  request_id: string
  content: string
  other: AuditOther | null
}

export type AuditScope = 'all' | 'self'

export interface AuditListParams {
  page: number
  pageSize: number
  category?: AuditCategory
  success?: boolean
  username?: string
  requestId?: string
  startTime?: number // unix seconds
  endTime?: number // unix seconds
}

export interface AuditListResponse {
  items: AuditLog[]
  total: number
}

export async function fetchAuditLogs(
  scope: AuditScope,
  params: AuditListParams,
  signal?: AbortSignal
): Promise<AuditListResponse> {
  const search = new URLSearchParams()
  search.set('p', String(params.page))
  search.set('page_size', String(params.pageSize))
  if (params.category) search.set('category', params.category)
  if (params.success !== undefined)
    search.set('success', String(params.success))
  if (scope === 'all' && params.username)
    search.set('username', params.username)
  if (params.requestId) search.set('request_id', params.requestId)
  if (params.startTime) search.set('start_timestamp', String(params.startTime))
  if (params.endTime) search.set('end_timestamp', String(params.endTime))

  const base = scope === 'all' ? '/api/audit' : '/api/audit/self'
  const res = await api.get<ApiResult<AuditListResponse>>(
    `${base}?${search.toString()}`,
    { ...CALLER_HANDLED_REQUEST_CONFIG, signal }
  )
  return unwrapApiResult(res.data, 'Failed to load audit records')
}
