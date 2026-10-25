import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  getRequestErrorMessage,
  type ApiRequestConfig,
} from '@/lib/api'
import type {
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

// The backend committed the values but this process could not apply them
// (usually the values themselves fail to load); see writeOptionSaveError.
export class OptionSavedNotPublishedError extends Error {
  readonly saved = true
}

// Option writes answer invalid keys/values with HTTP 400 and a message; keep
// that message instead of axios' generic "Request failed with status code".
async function putOptions(
  path: string,
  body: unknown,
  fallback: string
): Promise<UpdateOptionResponse> {
  let res
  try {
    res = await api.put<UpdateOptionResponse>(
      path,
      body,
      CALLER_HANDLED_REQUEST_CONFIG
    )
  } catch (error) {
    throw new Error(getRequestErrorMessage(error, fallback), { cause: error })
  }
  if (!res.data.success) {
    const message = res.data.message || fallback
    if (res.data.data?.saved) throw new OptionSavedNotPublishedError(message)
    throw new Error(message)
  }
  return res.data
}

export function updateSystemOption(request: UpdateOptionRequest) {
  return putOptions('/api/option/', request, 'Failed to update setting')
}

export function updateSystemOptionsBulk(request: UpdateOptionsBulkRequest) {
  return putOptions('/api/option/bulk', request, 'Failed to update settings')
}

// eflow: 上游 rc 的支付合规确认端点。确认后后端写入 payment_setting.compliance_confirmed
// 等 option，解锁支付/兑换码/邀请返利。需 dashboard session（不接受 access token）。
export async function confirmPaymentCompliance() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/payment_compliance',
    { confirmed: true },
    { skipBusinessError: true } satisfies ApiRequestConfig
  )
  return res.data
}

export async function resetModelRatios() {
  const res = await api.post<UpdateOptionResponse>(
    '/api/option/rest_model_ratio',
    undefined,
    CALLER_HANDLED_REQUEST_CONFIG
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
    request,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}
