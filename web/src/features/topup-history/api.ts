import { api } from '@/lib/api'
import type { TopupListResponse, TopupStatus } from './types'

type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

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
  params: TopupListParams
): Promise<TopupListResponse> {
  const search = new URLSearchParams()
  search.set('p', String(params.page))
  search.set('page_size', String(params.pageSize))
  if (params.keyword) search.set('keyword', params.keyword)
  if (params.status) search.set('status', params.status)
  if (params.startTime) search.set('start_time', String(params.startTime))
  if (params.endTime) search.set('end_time', String(params.endTime))

  const base = params.admin ? '/api/user/topup' : '/api/user/topup/self'
  const res = await api.get<ApiResponse<TopupListResponse>>(
    `${base}?${search.toString()}`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to load topup history')
  }
  return res.data.data
}

export async function adminCompleteTopup(tradeNo: string): Promise<boolean> {
  const res = await api.post<ApiResponse<unknown>>('/api/user/topup/complete', {
    trade_no: tradeNo,
  })
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to complete topup')
  }
  return true
}
