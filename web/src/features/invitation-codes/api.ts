import { api } from '@/lib/api'
import type {
  InvitationCode,
  InvitationCodeListResponse,
  InvitationCodeSavePayload,
  InvitationCodeUsage,
  MyInvitationCodeQuota,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

export type InvitationCodeListParams = {
  page: number
  pageSize: number
  keyword?: string
}

export async function fetchInvitationCodes(
  params: InvitationCodeListParams
): Promise<InvitationCodeListResponse> {
  const search = new URLSearchParams()
  search.set('p', String(params.page))
  search.set('page_size', String(params.pageSize))
  const path =
    params.keyword && params.keyword.trim()
      ? `/api/invitation_code/search?keyword=${encodeURIComponent(
          params.keyword.trim()
        )}&${search.toString()}`
      : `/api/invitation_code/?${search.toString()}`
  const res = await api.get<ApiResponse<InvitationCodeListResponse>>(path)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load invitation codes')
  }
  return res.data.data
}

export async function fetchInvitationCode(id: number): Promise<InvitationCode> {
  const res = await api.get<ApiResponse<InvitationCode>>(
    `/api/invitation_code/${id}`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load invitation code')
  }
  return res.data.data
}

export async function fetchInvitationCodeUsages(
  id: number
): Promise<InvitationCodeUsage[]> {
  const res = await api.get<ApiResponse<InvitationCodeUsage[] | null>>(
    `/api/invitation_code/${id}/usages`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load usages')
  }
  return res.data.data ?? []
}

export async function createInvitationCode(
  payload: InvitationCodeSavePayload
): Promise<string[]> {
  const res = await api.post<ApiResponse<string[] | null>>(
    '/api/invitation_code/',
    payload
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to create invitation code')
  }
  return res.data.data ?? []
}

export async function updateInvitationCode(
  payload: InvitationCodeSavePayload
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    '/api/invitation_code/',
    payload
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update invitation code')
  }
  return true
}

export async function deleteInvitationCode(id: number): Promise<boolean> {
  const res = await api.delete<ApiResponse<unknown>>(
    `/api/invitation_code/${id}`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to delete invitation code')
  }
  return true
}

export async function deleteInvalidInvitationCodes(): Promise<number> {
  const res = await api.delete<ApiResponse<number>>('/api/invitation_code/invalid')
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to clean invalid codes')
  }
  return res.data.data ?? 0
}

// ---- 普通用户视角 ----
// 三个 endpoint 走 /api/user/invitation_codes，靠 UserAuth 鉴权。

export async function fetchMyInvitationCodes(): Promise<InvitationCode[]> {
  const res = await api.get<ApiResponse<InvitationCode[] | null>>(
    '/api/user/invitation_codes'
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load my invitation codes')
  }
  return res.data.data ?? []
}

export async function fetchMyInvitationCodeQuota(): Promise<MyInvitationCodeQuota> {
  const res = await api.get<ApiResponse<MyInvitationCodeQuota>>(
    '/api/user/invitation_codes/quota'
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load quota')
  }
  return res.data.data
}

export type GenerateMyInvitationCodePayload = {
  // eflow: 客服自定义参数。普通用户路径下省略整个 body。
  count?: number
  max_uses?: number
  expired_time?: number // unix sec, 0 = 永久
  name?: string
}

export async function generateMyInvitationCode(
  payload?: GenerateMyInvitationCodePayload
): Promise<InvitationCode | InvitationCode[]> {
  // eflow: 后端单条返单对象、批量返数组；调用方按 Array.isArray 分支。
  const res = await api.post<ApiResponse<InvitationCode | InvitationCode[]>>(
    '/api/user/invitation_codes',
    payload ?? {}
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to generate invitation code')
  }
  return res.data.data
}
