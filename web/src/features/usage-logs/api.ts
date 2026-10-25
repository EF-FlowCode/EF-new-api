import axios from 'axios'
import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import { buildQueryParams } from './lib/utils'
import type {
  GetLogsParams,
  GetLogsResponse,
  GetLogStatsParams,
  GetLogStatsResponse,
  GetMidjourneyLogsParams,
  GetTaskLogsParams,
} from './types'

// ============================================================================
// Generic API Helpers
// ============================================================================

function buildApiPath(endpoint: string, isAdmin: boolean): string {
  return isAdmin ? endpoint : `${endpoint}/self`
}

async function fetchLogs<T>(
  endpoint: string,
  params: T,
  isAdmin: boolean
): Promise<GetLogsResponse> {
  const paramRecord = params as unknown as Record<string, unknown>
  const queryParams = buildQueryParams({
    p: paramRecord.p || 1,
    page_size: paramRecord.page_size || 20,
    ...params,
  })
  const path = buildApiPath(endpoint, isAdmin)
  // The logs table (common, drawing and task logs) reports success=false itself,
  // so only the business toast is skipped; HTTP errors such as the search rate
  // limit's 429 still get the interceptor toast because the table does not catch them.
  const res = await api.get(`${path}?${queryParams}`, {
    skipBusinessError: true,
  })
  return res.data
}

async function fetchLogStats<T>(
  endpoint: string,
  params: T,
  isAdmin: boolean
): Promise<GetLogStatsResponse> {
  const queryParams = buildQueryParams(
    params as unknown as Record<string, unknown>
  )
  const path = buildApiPath(endpoint, isAdmin)
  const res = await api.get(`${path}/stat?${queryParams}`)
  return res.data
}

// ============================================================================
// Common Log APIs
// ============================================================================

export const getAllLogs = (params: GetLogsParams = {}) =>
  fetchLogs('/api/log', params, true)

export const getUserLogs = (
  params: Omit<GetLogsParams, 'username' | 'channel'> = {}
) => fetchLogs('/api/log', params, false)

export const getLogStats = (params: GetLogStatsParams = {}) =>
  fetchLogStats('/api/log', params, true)

export const getUserLogStats = (
  params: Omit<GetLogStatsParams, 'username' | 'channel'> = {}
) => fetchLogStats('/api/log', params, false)

// 导出以 blob 请求，后端失败时回的是 JSON（HTTP 200 或 4xx/5xx），要读出 message。
async function readBlobErrorMessage(
  data: unknown,
  isJson = data instanceof Blob && data.type.includes('json')
): Promise<string | undefined> {
  if (!(data instanceof Blob) || !isJson) return undefined
  try {
    const body = JSON.parse(await data.text()) as { message?: unknown }
    return typeof body.message === 'string' && body.message
      ? body.message
      : undefined
  } catch {
    return undefined
  }
}

// eflow: 管理员导出指定用户时，query 带 user_id 并改走 /api/log/export(-async)。
function logExportPath(query: URLSearchParams, suffix: string): string {
  const base = query.has('user_id') ? '/api/log/export' : '/api/log/self/export'
  return `${base}${suffix}?${query.toString()}`
}

// eflow: 使用日志导出（报账）。后端回 ZIP，这里取 blob + 文件名；
// 后端报错（JSON）时抛出带后端 message 的 Error，不把 JSON 当 ZIP 下载。
// query 带 user_id 时为管理员导出指定用户。
export async function exportUserLogs(
  query: URLSearchParams
): Promise<{ blob: Blob; filename: string }> {
  let res
  try {
    res = await api.get(logExportPath(query, ''), {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      responseType: 'blob',
      disableDuplicate: true,
    } as never)
  } catch (error) {
    const message = axios.isAxiosError(error)
      ? await readBlobErrorMessage(error.response?.data)
      : undefined
    throw message ? new Error(message) : error
  }
  const blob = res.data as Blob
  const contentType = String(res.headers?.['content-type'] ?? blob.type ?? '')
  if (contentType.includes('json') || blob.type.includes('json')) {
    throw new Error((await readBlobErrorMessage(blob, true)) || 'Export failed')
  }
  const cd = (res.headers?.['content-disposition'] as string | undefined) || ''
  const m = /filename="?([^"]+)"?/.exec(cd)
  return {
    blob,
    filename: m ? decodeURIComponent(m[1]) : 'usage-logs.zip',
  }
}

// eflow: 离线导出——后台异步生成 ZIP 并发到邮箱，立即返回提示文案（非 blob）。
export async function requestOfflineExport(
  query: URLSearchParams
): Promise<{ success: boolean; message?: string }> {
  const res = await api.get(logExportPath(query, '-async'))
  return res.data
}

// ============================================================================
// Midjourney (Drawing) Logs API
// ============================================================================

export const getAllMidjourneyLogs = (params: GetMidjourneyLogsParams) =>
  fetchLogs('/api/mj', params, true)

export const getUserMidjourneyLogs = (params: GetMidjourneyLogsParams) =>
  fetchLogs('/api/mj', params, false)

// ============================================================================
// Task Logs API
// ============================================================================

export const getAllTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, true)

export const getUserTaskLogs = (params: GetTaskLogsParams) =>
  fetchLogs('/api/task', params, false)
