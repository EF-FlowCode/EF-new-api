import Papa from 'papaparse'
import { api, unwrapApiResult } from '@/lib/api'
import { parseQuotaFromDollars, quotaUnitsToEditableAmount } from '@/lib/format'
import type { GetLogsParams } from '@/features/usage-logs/types'

export type RefundBatch = {
  id: string
  admin_id: number
  reason: string
  status: 'preview' | 'executed'
  quota: number
  count: number
  created_at: number
  executed_at: number
}
export type RefundItem = {
  log_id: number
  request_id: string
  user_id: number
  username: string
  quota: number
  original_quota?: number
  created_at?: number
  model_name?: string
  status: string
  refund_batch_id?: string
}
export type RefundView = RefundBatch & {
  logs_pending: number
  items: RefundItem[]
  users: { user_id: number; username: string; quota: number; count: number }[]
  receipts: { source_log_id: number; quota: number; token_quota: number }[]
}
export type RefundSelection = {
  reason: string
  include_client_gone: boolean
  log_ids?: number[]
  request_ids?: string[]
  upstream_request_ids?: string[]
  filter?: ReturnType<typeof refundFilter> & {
    upstream_request_id?: string
    exclude_groups?: string[]
  }
  amounts?: Record<string, number>
}
export type RefundCandidateQuery = {
  user_id?: number
  username?: string
  start_timestamp: number
  end_timestamp: number
  model_name?: string
  request_ids?: string[]
  include_client_gone: boolean
}
export type RefundCandidate = {
  log_id: number
  request_id: string
  created_at: number
  model_name: string
  token_name: string
  group: string
  channel: number
  prompt_tokens: number
  completion_tokens: number
  quota: number
  status: string
  refund_batch_id?: string
  refunded_quota?: number
}
export type RefundCandidates = {
  user_id: number
  username: string
  items: RefundCandidate[]
  missing_request_ids?: string[]
}

// Billing anomaly categories written to logs.billing_anomaly at settlement.
// 3 (no upstream usage) settled at zero, so it is audit-only.
export const BILLING_ANOMALY = {
  inputEstimated: 1,
  outputEstimated: 2,
  noUpstreamUsage: 3,
} as const
export type BillingAnomalyQuery = {
  start_timestamp: number
  end_timestamp: number
  anomaly: number
  // Charged logs with at most 1 output token, flagged or not; logs written
  // before the anomaly flag shipped (2026-09-28) are only reachable this way.
  low_output?: boolean
  user_id?: number
  channel?: number
  model_name?: string
}
export type BillingAnomalyItem = {
  log_id: number
  request_id: string
  created_at: number
  user_id: number
  username: string
  model_name: string
  group: string
  channel: number
  prompt_tokens: number
  completion_tokens: number
  use_time: number
  quota: number
  anomaly: number
  status: string
  refund_batch_id?: string
  refunded_quota?: number
}
export type BillingAnomalyUser = {
  user_id: number
  username: string
  count: number
  quota: number
  refunded_quota: number
  by_anomaly: Record<string, number>
}
export type BillingAnomalies = {
  items: BillingAnomalyItem[]
  users: BillingAnomalyUser[]
}

// Only charged, still-eligible anomalies can move into a refund batch.
export function isRefundableAnomaly(item: BillingAnomalyItem) {
  return (
    item.status === 'eligible' &&
    item.quota > 0 &&
    item.anomaly !== BILLING_ANOMALY.noUpstreamUsage
  )
}

export function refundFilter(params: GetLogsParams, maxCompletion?: number) {
  return {
    start_timestamp: params.start_timestamp,
    end_timestamp: params.end_timestamp,
    group: params.group,
    channel: params.channel,
    username: params.username,
    user_id: params.user_id,
    model_name: params.model_name,
    token_name: params.token_name,
    request_id: params.request_id,
    type: params.type,
    max_completion_tokens: maxCompletion,
  }
}

// Request IDs pasted from a spreadsheet column, a CSV line or a JSON array.
// The column header and duplicates are dropped.
export function parseRequestIDs(text: string) {
  const ids = text
    .split(/[\s,;，；"'[\]]+/)
    .filter((id) => id && !/^request_?id$/i.test(id))
  const unique = [...new Set(ids)]
  if (unique.length > 5000 || unique.some((id) => id.length > 128))
    throw new Error('Invalid identifier list')
  return unique
}

// Groups excluded from filter scans, kept per browser so groups that never
// produce output tokens (embedding/rerank) stay out of every scan.
export const REFUND_EXCLUDE_GROUPS_KEY = 'usage-refund-exclude-groups'

export function parseExcludeGroups(text: string) {
  const groups = [...new Set(text.split(/[\s,;，；]+/).filter(Boolean))]
  if (groups.length > 50 || groups.some((group) => group.length > 64))
    throw new Error('Invalid excluded group list')
  return groups
}

// Operators type return amounts in the site's display currency (the same unit
// the page shows); the API takes integer quota.
export type RefundAmountConverter = {
  toQuota: (amount: number) => number
  fromQuota: (quota: number) => number
}

export const displayAmountConverter: RefundAmountConverter = {
  toQuota: parseQuotaFromDollars,
  fromQuota: quotaUnitsToEditableAmount,
}

// refundQuotaFromInput converts one typed amount to quota, or null when it is
// not a positive amount within the original charge. Empty input returns the
// full charge, and so does typing the displayed charge itself: display rounding
// must not turn "return all" into a return that is a few quota short.
export function refundQuotaFromInput(
  item: RefundCandidate,
  text: string,
  convert: RefundAmountConverter = displayAmountConverter
): number | null {
  const trimmed = text.trim()
  if (trimmed === '') return item.quota
  if (!/^\d+(\.\d+)?$/.test(trimmed)) return null
  const amount = Number(trimmed)
  if (!Number.isFinite(amount) || amount <= 0) return null
  if (amount === convert.fromQuota(item.quota)) return item.quota
  const quota = convert.toQuota(amount)
  return Number.isSafeInteger(quota) && quota >= 1 && quota <= item.quota
    ? quota
    : null
}

// Manual return: every picked log must be eligible with a valid amount.
// Full-charge picks omit `amounts`; the others carry the converted quota.
export function manualRefundSelection(
  candidates: RefundCandidate[],
  picks: Record<number, string>,
  convert: RefundAmountConverter = displayAmountConverter
) {
  const byID = new Map(candidates.map((item) => [item.log_id, item]))
  const logIDs: number[] = []
  const amounts: Record<string, number> = {}
  for (const [key, raw] of Object.entries(picks)) {
    const item = byID.get(Number(key))
    if (!item || item.status !== 'eligible')
      throw new Error('Invalid refund amount')
    const quota = refundQuotaFromInput(item, raw, convert)
    if (quota === null) throw new Error('Invalid refund amount')
    logIDs.push(item.log_id)
    if (quota !== item.quota) amounts[String(item.log_id)] = quota
  }
  if (!logIDs.length) throw new Error('Select at least one record')
  if (logIDs.length > 5000) throw new Error('Invalid identifier list')
  logIDs.sort((a, b) => a - b)
  return Object.keys(amounts).length
    ? { log_ids: logIDs, amounts }
    : { log_ids: logIDs }
}

const options = { skipErrorHandler: true, skipBusinessError: true }
export async function previewRefund(
  selection: RefundSelection
): Promise<RefundView> {
  const response = await api.post(
    '/api/log/refunds/preview',
    selection,
    options
  )
  return unwrapApiResult<RefundView>(
    response.data,
    'Refund preview failed; no balance was changed'
  )
}
export async function listRefundCandidates(
  query: RefundCandidateQuery
): Promise<RefundCandidates> {
  const response = await api.post('/api/log/refunds/candidates', query, options)
  return unwrapApiResult<RefundCandidates>(
    response.data,
    'Failed to load refund candidates'
  )
}
export async function listBillingAnomalies(
  query: BillingAnomalyQuery
): Promise<BillingAnomalies> {
  const response = await api.post('/api/log/refunds/anomalies', query, options)
  return unwrapApiResult<BillingAnomalies>(
    response.data,
    'Failed to load billing anomalies'
  )
}
export async function executeRefund(view: RefundView): Promise<RefundView> {
  const response = await api.post(
    `/api/log/refunds/${view.id}/execute`,
    { expected_quota: view.quota, expected_count: view.count },
    options
  )
  return unwrapApiResult<RefundView>(response.data, 'Balance return failed')
}
export async function getRefund(id: string): Promise<RefundView> {
  const response = await api.get(
    `/api/log/refunds/${encodeURIComponent(id)}`,
    options
  )
  return unwrapApiResult<RefundView>(response.data, 'Failed to load batch')
}
export async function listRefunds(): Promise<RefundBatch[]> {
  const response = await api.get('/api/log/refunds/', options)
  return unwrapApiResult<RefundBatch[]>(response.data, 'Failed to load batches')
}

export function downloadRefund(view: RefundView) {
  const csv = Papa.unparse(
    view.items.map((item) => ({
      ...item,
      batch_id: view.id,
      reason: view.reason,
      status:
        view.status === 'executed' && item.status === 'eligible'
          ? 'refunded'
          : item.status,
    })),
    { escapeFormulae: true }
  )
  const url = URL.createObjectURL(
    new Blob(['\ufeff', csv], { type: 'text/csv;charset=utf-8' })
  )
  const link = document.createElement('a')
  link.href = url
  link.download = `usage-refund-${view.id}.csv`
  link.click()
  URL.revokeObjectURL(url)
}
