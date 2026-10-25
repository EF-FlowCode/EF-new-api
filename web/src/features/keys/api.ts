import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import type {
  ApiKey,
  ApiResponse,
  GetApiKeysParams,
  GetApiKeysResponse,
  SearchApiKeysParams,
  ApiKeyFormData,
} from './types'

// ============================================================================
// API Key Management
// ============================================================================

// Get paginated API keys list
export async function getApiKeys(
  params: GetApiKeysParams = {}
): Promise<GetApiKeysResponse> {
  const { p = 1, size = 10, sort_by, sort_order } = params
  const queryParams = new URLSearchParams({ p: String(p), size: String(size) })
  if (sort_by) queryParams.set('sort_by', sort_by)
  if (sort_order) queryParams.set('sort_order', sort_order)
  // Callers (keys table, chat-link key lookup) report success=false themselves,
  // so only the business toast is skipped; HTTP errors such as 429 still get the
  // interceptor toast because these callers do not catch them.
  const res = await api.get(`/api/token/?${queryParams.toString()}`, {
    skipBusinessError: true,
  })
  return res.data
}

// Search API keys by keyword or token (with pagination)
export async function searchApiKeys(
  params: SearchApiKeysParams
): Promise<{ success: boolean; message?: string; data?: ApiKey[] }> {
  const { keyword = '', token = '', p, size, sort_by, sort_order } = params
  const queryParams = new URLSearchParams()
  if (keyword) queryParams.set('keyword', keyword)
  if (token) queryParams.set('token', token)
  if (p != null) queryParams.set('p', String(p))
  if (size != null) queryParams.set('size', String(size))
  if (sort_by) queryParams.set('sort_by', sort_by)
  if (sort_order) queryParams.set('sort_order', sort_order)
  const res = await api.get(`/api/token/search?${queryParams.toString()}`, {
    skipBusinessError: true,
  })
  return res.data
}

// Get single API key by ID
export async function getApiKey(id: number): Promise<ApiResponse<ApiKey>> {
  const res = await api.get(`/api/token/${id}`)
  return res.data
}

// Create a new API key
export async function createApiKey(
  data: ApiKeyFormData
): Promise<ApiResponse<ApiKey>> {
  const res = await api.post('/api/token/', data, CALLER_HANDLED_REQUEST_CONFIG)
  return res.data
}

// Update an existing API key
export async function updateApiKey(
  data: ApiKeyFormData & { id: number }
): Promise<ApiResponse<ApiKey>> {
  const res = await api.put('/api/token/', data, CALLER_HANDLED_REQUEST_CONFIG)
  return res.data
}

// Delete a single API key
export async function deleteApiKey(id: number): Promise<ApiResponse> {
  const res = await api.delete(
    `/api/token/${id}/`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Batch delete multiple API keys
export async function batchDeleteApiKeys(
  ids: number[]
): Promise<ApiResponse<number>> {
  const res = await api.post(
    '/api/token/batch',
    { ids },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Update API key status (enable/disable)
export async function updateApiKeyStatus(
  id: number,
  status: number
): Promise<ApiResponse<ApiKey>> {
  const res = await api.put(
    '/api/token/?status_only=true',
    { id, status },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Fetch the real (unmasked) key for a token by ID
export async function fetchTokenKey(
  id: number
): Promise<{ success: boolean; message?: string; data?: { key: string } }> {
  const res = await api.post(
    `/api/token/${id}/key`,
    undefined,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Batch fetch real (unmasked) keys for multiple tokens
export async function fetchTokenKeysBatch(ids: number[]): Promise<{
  success: boolean
  message?: string
  data?: { keys: Record<number, string> }
}> {
  const res = await api.post(
    '/api/token/batch/keys',
    { ids },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}
