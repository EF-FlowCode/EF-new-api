import { api } from '@/lib/api'
import type {
  DeleteLogsResponse,
  FetchUpstreamRatiosRequest,
  SystemOptionsResponse,
  UpdateOptionRequest,
  UpdateOptionResponse,
  UpdateOptionsBulkRequest,
  UpstreamChannelsResponse,
  UpstreamRatiosResponse,
} from './types'

export async function getSystemOptions() {
  const res = await api.get<SystemOptionsResponse>('/api/option/')
  return res.data
}

export async function updateSystemOption(request: UpdateOptionRequest) {
  const res = await api.put<UpdateOptionResponse>('/api/option/', request)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update setting')
  }
  return res.data
}

export async function updateSystemOptionsBulk(
  request: UpdateOptionsBulkRequest
) {
  const res = await api.put<UpdateOptionResponse>('/api/option/bulk', request)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update settings')
  }
  return res.data
}

// eflow: 上游 rc 的支付合规确认端点。确认后后端写入 payment_setting.compliance_confirmed
// 等 option，解锁支付/兑换码/邀请返利。需 dashboard session（不接受 access token）。
export async function confirmPaymentCompliance() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/payment_compliance',
    { confirmed: true }
  )
  return res.data
}

export async function deleteLogsBefore(targetTimestamp: number) {
  const res = await api.delete<DeleteLogsResponse>('/api/log/', {
    params: { target_timestamp: targetTimestamp },
  })
  return res.data
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio'
  )
  return res.data
}

export async function getUpstreamChannels() {
  const res = await api.get<UpstreamChannelsResponse>(
    '/api/ratio_sync/channels'
  )
  return res.data
}

export async function fetchUpstreamRatios(request: FetchUpstreamRatiosRequest) {
  const res = await api.post<UpstreamRatiosResponse>(
    '/api/ratio_sync/fetch',
    request
  )
  return res.data
}
