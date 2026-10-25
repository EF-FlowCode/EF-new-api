import axios, {
  type AxiosInstance,
  type AxiosRequestConfig,
  type InternalAxiosRequestConfig,
} from 'axios'
import i18next from 'i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { isMaintenanceError, setMaintenance } from '@/lib/maintenance'
import { securityProofHeaders } from './secure-verification'

declare module 'axios' {
  // Keep custom request controls available on every Axios method signature.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @typescript-eslint/no-unused-vars
  interface AxiosRequestConfig<D = any> {
    disableDuplicate?: boolean
    skipBusinessError?: boolean
    skipErrorHandler?: boolean
    skipErrorNotification?: boolean
  }
}

export type ApiResult<T = unknown> =
  | { success: true; message?: string; data: T }
  | { success: false; message?: string; data?: T }

export type ApiRequestConfig<D = unknown> = AxiosRequestConfig<D>

// Keep the literal type: a widened AxiosRequestConfig would carry generic
// fields (e.g. paramsSerializer) that conflict with typed params when spread.
export const CALLER_HANDLED_REQUEST_CONFIG = {
  skipBusinessError: true,
  skipErrorNotification: true,
} as const satisfies ApiRequestConfig

export function unwrapApiResult<T>(
  result: ApiResult<T>,
  fallbackMessage: string
): T {
  if (!result.success) {
    throw new Error(result.message || fallbackMessage)
  }
  return result.data
}

export function getRequestErrorMessage(
  error: unknown,
  fallbackMessage: string
): string {
  if (axios.isAxiosError<{ message?: unknown }>(error)) {
    const responseMessage = error.response?.data?.message
    if (typeof responseMessage === 'string' && responseMessage) {
      return responseMessage
    }
  }
  if (error instanceof Error && error.message) {
    return error.message
  }
  return fallbackMessage
}

type ApiErrorInterceptorOptions = {
  notifyError?: (message: string) => unknown
  resetAuth?: () => void
}

// ============================================================================
// Axios Instance Configuration
// ============================================================================

// Base URL: empty string for same-origin API requests
const baseURL = ''

// Create axios instance with default config
export const api = axios.create({
  baseURL,
  withCredentials: true, // Include cookies in cross-origin requests
  headers: {
    'Cache-Control': 'no-store', // Prevent caching
  },
})

// ============================================================================
// Request Deduplication
// ============================================================================

// Deduplicate concurrent GET requests to the same URL
// Prevents multiple identical requests from being sent simultaneously
const inFlightGet = new Map<string, Promise<unknown>>()
const originalGet = api.get.bind(api)

api.get = ((url: string, config: ApiRequestConfig = {}) => {
  const disableDuplicate = config.disableDuplicate
  // A caller that supplies an AbortSignal owns this request's lifecycle. Sharing
  // another caller's promise would let aborting the older request cancel the
  // newer one as well.
  if (disableDuplicate || config.signal) return originalGet(url, config)

  const params = config.params ? JSON.stringify(config.params) : '{}'
  const key = `${url}?${params}`

  // Return existing in-flight request if available
  if (inFlightGet.has(key)) return inFlightGet.get(key)!

  // Create new request and clean up after completion
  const req = originalGet(url, config).finally(() => inFlightGet.delete(key))
  inFlightGet.set(key, req)
  return req
}) as typeof api.get

// ============================================================================
// Response Interceptor
// ============================================================================

// Handle business logic errors and HTTP errors globally.
export function installApiErrorInterceptor(
  client: AxiosInstance,
  options: ApiErrorInterceptorOptions = {}
) {
  const notifyError = options.notifyError ?? ((message) => toast.error(message))
  const resetAuth =
    options.resetAuth ?? (() => useAuthStore.getState().auth.reset())

  return client.interceptors.response.use(
    (response) => {
      const skipBusiness = (
        response.config as InternalAxiosRequestConfig & ApiRequestConfig
      ).skipBusinessError

      // Unified business response format: { success, message, data }
      if (
        !skipBusiness &&
        response &&
        response.data &&
        typeof response.data.success === 'boolean' &&
        !response.data.success
      ) {
        notifyError(response.data.message || i18next.t('Request failed'))
      }
      return response
    },
    (error) => {
      if (isMaintenanceError(error)) {
        setMaintenance(true)
        return Promise.reject(error)
      }
      const skipHandler = error?.config?.skipErrorHandler
      if (!skipHandler) {
        const skipNotification = error?.config?.skipErrorNotification
        const status = error?.response?.status

        if (status === 401) {
          if (!skipNotification) {
            notifyError(i18next.t('Session expired!'))
          }
          try {
            resetAuth()
          } catch {
            /* empty */
          }
        } else if (!skipNotification) {
          const message = getRequestErrorMessage(
            error,
            i18next.t('Request error')
          )
          notifyError(i18next.exists(message) ? i18next.t(message) : message)
        }
      }
      return Promise.reject(error)
    }
  )
}

installApiErrorInterceptor(api)

// ============================================================================
// Common Headers Utility
// ============================================================================

/**
 * Get user ID from localStorage
 */
function getUserId(): string | null {
  try {
    if (typeof window !== 'undefined') {
      return window.localStorage.getItem('uid')
    }
  } catch {
    /* empty */
  }
  return null
}

/**
 * Get common request headers (for both axios and SSE requests)
 */
export function getCommonHeaders(): Record<string, string> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }

  const uid = getUserId()
  if (uid) {
    headers['New-Api-User'] = uid
  }

  return headers
}

// ============================================================================
// Request Interceptor
// ============================================================================

// Attach user ID header for all requests
api.interceptors.request.use((config) => {
  const uid = getUserId()
  if (uid) {
    // Custom header for user identification
    ;(config.headers as Record<string, string>)['New-Api-User'] = uid
  }
  return config
})

// ============================================================================
// Common API Functions
// ============================================================================

// ----------------------------------------------------------------------------
// User APIs
// ----------------------------------------------------------------------------

// Get current user info
export async function getSelf() {
  const res = await api.get('/api/user/self', {
    // Avoid global 401 toast during guards/preloads
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}

// Get user available models
export async function getUserModels(): Promise<{
  success: boolean
  message?: string
  data?: string[]
}> {
  const res = await api.get('/api/user/models')
  return res.data
}

// Get user groups with descriptions and ratios
export async function getUserGroups(): Promise<{
  success: boolean
  message?: string
  data?: Record<
    string,
    { desc: string; ratio: number | string; available?: boolean }
  >
}> {
  const res = await api.get('/api/user/self/groups')
  return res.data
}

export type WebGroupRegionAccess = {
  region: string
  restricted: boolean
  popup?: {
    display_name?: string
    title?: string
    content?: string
    notice?: string
    button?: string
    version?: string
  }
}

export async function getWebGroupRegionAccess(): Promise<{
  success: boolean
  message?: string
  data?: WebGroupRegionAccess
}> {
  const res = await api.get('/api/web-group-region')
  return res.data
}

// ----------------------------------------------------------------------------
// System APIs
// ----------------------------------------------------------------------------

// Get system status
export async function getStatus() {
  const res = await api.get('/api/status')
  return res.data?.data as Record<string, unknown>
}

// Get system notice
export async function getNotice(): Promise<{
  success: boolean
  message?: string
  data?: string
}> {
  const res = await api.get('/api/notice')
  return res.data
}

// ----------------------------------------------------------------------------
// 2FA Management APIs
// ----------------------------------------------------------------------------

// Get 2FA status
export async function get2FAStatus(config: ApiRequestConfig = {}) {
  const res = await api.get('/api/user/2fa/status', config)
  return res.data
}

// Setup 2FA (needs a 2fa.setup proof)
export async function setup2FA(proofToken?: string) {
  const res = await api.post('/api/user/2fa/setup', undefined, {
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}

// Enable 2FA with verification code
export async function enable2FA(code: string) {
  const res = await api.post('/api/user/2fa/enable', { code })
  return res.data
}

// Disable 2FA (needs a 2fa.disable proof)
export async function disable2FA(proofToken?: string) {
  const res = await api.post('/api/user/2fa/disable', undefined, {
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}

// Regenerate 2FA backup codes (needs a 2fa.backup_codes.regenerate proof)
export async function regenerate2FABackupCodes(proofToken?: string) {
  const res = await api.post('/api/user/2fa/backup_codes', undefined, {
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}
