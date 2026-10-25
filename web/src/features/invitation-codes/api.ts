import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'
import type {
  InvitationCode,
  InvitationCodeListResponse,
  InvitationCodeSavePayload,
  InvitationCodeUsage,
  MyInvitationCodeQuota,
} from './types'

export type InvitationCodeListParams = {
  page: number
  pageSize: number
  keyword?: string
}

export async function fetchInvitationCodes(
  params: InvitationCodeListParams,
  signal?: AbortSignal
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
  const res = await api.get<ApiResult<InvitationCodeListResponse>>(path, {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    signal,
  })
  return unwrapApiResult(res.data, 'Failed to load invitation codes')
}

export async function fetchInvitationCode(id: number): Promise<InvitationCode> {
  const res = await api.get<ApiResult<InvitationCode>>(
    `/api/invitation_code/${id}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load invitation code')
}

export async function fetchInvitationCodeUsages(
  id: number
): Promise<InvitationCodeUsage[]> {
  const res = await api.get<ApiResult<InvitationCodeUsage[] | null>>(
    `/api/invitation_code/${id}/usages`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load usages') ?? []
}

export async function createInvitationCode(
  payload: InvitationCodeSavePayload
): Promise<string[]> {
  const res = await api.post<ApiResult<string[] | null>>(
    '/api/invitation_code/',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to create invitation code') ?? []
}

export async function updateInvitationCode(
  payload: InvitationCodeSavePayload
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    '/api/invitation_code/',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update invitation code')
  return true
}

export async function deleteInvitationCode(id: number): Promise<boolean> {
  const res = await api.delete<ApiResult<unknown>>(
    `/api/invitation_code/${id}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to delete invitation code')
  return true
}

export async function deleteInvalidInvitationCodes(): Promise<number> {
  const res = await api.delete<ApiResult<number>>(
    '/api/invitation_code/invalid',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to clean invalid codes') ?? 0
}

// ---- 普通用户视角 ----
// 三个 endpoint 走 /api/user/invitation_codes，靠 UserAuth 鉴权。

export async function fetchMyInvitationCodes(): Promise<InvitationCode[]> {
  const res = await api.get<ApiResult<InvitationCode[] | null>>(
    '/api/user/invitation_codes',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load my invitation codes') ?? []
}

export async function fetchMyInvitationCodeQuota(): Promise<MyInvitationCodeQuota> {
  const res = await api.get<ApiResult<MyInvitationCodeQuota>>(
    '/api/user/invitation_codes/quota',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load quota')
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
  const res = await api.post<ApiResult<InvitationCode | InvitationCode[]>>(
    '/api/user/invitation_codes',
    payload ?? {},
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to generate invitation code')
}
