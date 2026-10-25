import { api, unwrapApiResult } from '@/lib/api'
import type { QuotaDataItem, UptimeGroupResult } from './types'

export type AccountSummary = {
  quota: number
  used_quota: number
  refunded_quota: number
  net_used_quota: number
  request_count: number
}

export type RefundRecordKind = 'usage' | 'task' | 'ticket'
export type PersonalRefundRecord = {
  id: string
  kind: RefundRecordKind
  created_at: number
  quota: number
  reason: string
  status: 'completed' | 'pending' | 'rejected'
  request_id?: string
  ticket_id?: number
}

const accountRequestOptions = {
  skipErrorHandler: true,
  skipBusinessError: true,
}

export async function getAccountSummary(signal?: AbortSignal) {
  const res = await api.get('/api/user/self/usage-summary', {
    ...accountRequestOptions,
    signal,
  })
  return unwrapApiResult<AccountSummary>(
    res.data,
    'Failed to load account summary'
  )
}

export async function getPersonalRefundRecords(
  kind: RefundRecordKind,
  page: number,
  signal?: AbortSignal
) {
  const res = await api.get('/api/user/self/refund-records', {
    ...accountRequestOptions,
    signal,
    params: { kind, p: page, page_size: 10 },
  })
  return unwrapApiResult<{ items: PersonalRefundRecord[]; has_more: boolean }>(
    res.data,
    'Failed to load refund records'
  )
}

// ============================================================================
// Dashboard APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Quota & Usage Data
// ----------------------------------------------------------------------------

// Get user quota data within a time range
// Admin users get all users' data by default (preserves existing behavior).
export async function getUserQuotaDates(
  params: {
    start_timestamp: number
    end_timestamp: number
    default_time?: string
    username?: string
  },
  isAdmin = false
) {
  const endpoint = isAdmin ? '/api/data' : '/api/data/self'
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    endpoint,
    { params }
  )
  return res.data
}

// ----------------------------------------------------------------------------
// System Monitoring
// ----------------------------------------------------------------------------

export async function getUserQuotaDataByUsers(params: {
  start_timestamp: number
  end_timestamp: number
}) {
  const res = await api.get<{ success: boolean; data: QuotaDataItem[] }>(
    '/api/data/users',
    { params }
  )
  return res.data
}

// Get uptime monitoring status for all services
export async function getUptimeStatus() {
  const res = await api.get<{ success: boolean; data: UptimeGroupResult[] }>(
    '/api/uptime/status'
  )
  return res.data
}
