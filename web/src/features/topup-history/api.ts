import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'
import type { TopupListResponse, TopupStatus } from './types'

export type TopupListParams = {
  page: number
  pageSize: number
  keyword?: string
  status?: TopupStatus
  startTime?: number // unix seconds
  endTime?: number // unix seconds
  admin?: boolean
}

export async function fetchTopups(
  params: TopupListParams,
  signal?: AbortSignal
): Promise<TopupListResponse> {
  const search = new URLSearchParams()
  search.set('p', String(params.page))
  search.set('page_size', String(params.pageSize))
  if (params.keyword) search.set('keyword', params.keyword)
  if (params.status) search.set('status', params.status)
  if (params.startTime) search.set('start_time', String(params.startTime))
  if (params.endTime) search.set('end_time', String(params.endTime))

  const base = params.admin ? '/api/user/topup' : '/api/user/topup/self'
  const res = await api.get<ApiResult<TopupListResponse>>(
    `${base}?${search.toString()}`,
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      signal,
    }
  )
  return unwrapApiResult(res.data, 'Failed to load topup history')
}

export async function adminCompleteTopup(tradeNo: string): Promise<boolean> {
  const res = await api.post<ApiResult<unknown>>(
    '/api/user/topup/complete',
    { trade_no: tradeNo },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to complete topup')
  return true
}
