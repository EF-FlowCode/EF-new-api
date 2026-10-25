import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import type {
  Redemption,
  ApiResponse,
  GetRedemptionsParams,
  GetRedemptionsResponse,
  SearchRedemptionsParams,
  RedemptionFormData,
} from './types'

// ============================================================================
// Redemption Code Management
// ============================================================================

// Get paginated redemption codes list
export async function getRedemptions(
  params: GetRedemptionsParams = {}
): Promise<GetRedemptionsResponse> {
  const { p = 1, page_size = 10, status, sort_by, sort_order } = params
  const qs = new URLSearchParams({
    p: String(p),
    page_size: String(page_size),
  })
  if (status) qs.set('status', status)
  if (sort_by) qs.set('sort_by', sort_by)
  if (sort_order) qs.set('sort_order', sort_order)
  const res = await api.get(`/api/redemption/?${qs.toString()}`)
  return res.data
}

// Search redemption codes by keyword
export async function searchRedemptions(
  params: SearchRedemptionsParams
): Promise<GetRedemptionsResponse> {
  const {
    keyword = '',
    status,
    p = 1,
    page_size = 10,
    sort_by,
    sort_order,
  } = params
  const qs = new URLSearchParams({
    keyword,
    p: String(p),
    page_size: String(page_size),
  })
  if (status) qs.set('status', status)
  if (sort_by) qs.set('sort_by', sort_by)
  if (sort_order) qs.set('sort_order', sort_order)
  const res = await api.get(`/api/redemption/search?${qs.toString()}`)
  return res.data
}

// Get single redemption code by ID
export async function getRedemption(
  id: number
): Promise<ApiResponse<Redemption>> {
  const res = await api.get(
    `/api/redemption/${id}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Create redemption code(s)
export async function createRedemption(
  data: RedemptionFormData
): Promise<ApiResponse<string[]>> {
  const res = await api.post('/api/redemption/', data)
  return res.data
}

// Update redemption code
export async function updateRedemption(
  data: RedemptionFormData & { id: number }
): Promise<ApiResponse<Redemption>> {
  const res = await api.put('/api/redemption/', data)
  return res.data
}

// Update redemption code status (enable/disable)
export async function updateRedemptionStatus(
  id: number,
  status: number
): Promise<ApiResponse<Redemption>> {
  const res = await api.put('/api/redemption/?status_only=true', { id, status })
  return res.data
}

// Delete a single redemption code
export async function deleteRedemption(id: number): Promise<ApiResponse> {
  const res = await api.delete(`/api/redemption/${id}/`)
  return res.data
}

// Delete invalid redemption codes (used, disabled, expired)
export async function deleteInvalidRedemptions(): Promise<ApiResponse<number>> {
  const res = await api.delete('/api/redemption/invalid')
  return res.data
}

// eflow: 批量删除选中的兑换码（软删除，一次最多 1000 个），返回实际删除条数。
export async function batchDeleteRedemptions(
  ids: number[]
): Promise<ApiResponse<number>> {
  const res = await api.post('/api/redemption/batch', { ids })
  return res.data
}
