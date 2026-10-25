import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import type { CustomOAuthBinding } from '@/lib/oauth'
import { securityProofHeaders } from '@/lib/secure-verification'
import type {
  ApiResponse,
  UserProfile,
  UpdateUserRequest,
  UpdateUserSettingsRequest,
  CheckinStatusResponse,
  CheckinResponse,
} from './types'

// ============================================================================
// User Profile APIs
// ============================================================================

/**
 * Get current user profile
 */
export async function getUserProfile(): Promise<ApiResponse<UserProfile>> {
  const res = await api.get('/api/user/self', CALLER_HANDLED_REQUEST_CONFIG)
  return res.data
}

/**
 * Update user profile
 */
/** Changing the password needs an account.password.change proof. */
export async function updateUserProfile(
  data: UpdateUserRequest,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.put('/api/user/self', data, {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}

/**
 * Update user settings
 */
export async function updateUserSettings(
  data: UpdateUserSettingsRequest
): Promise<ApiResponse> {
  const res = await api.put(
    '/api/user/setting',
    data,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Update interface language preference
 */
export async function updateUserLanguage(
  language: string
): Promise<ApiResponse> {
  const res = await api.put(
    '/api/user/self',
    { language },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Delete user account
 */
/** Deleting the account needs an account.delete proof. */
export async function deleteUserAccount(
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete('/api/user/self', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}

/**
 * Send email verification code
 */
export async function sendEmailVerification(
  email: string
): Promise<ApiResponse> {
  const params = new URLSearchParams({ email })
  const res = await api.get(
    `/api/user/verification?${params}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Bind email account
 */
/** Replacing an existing email needs an account.binding.bind proof. */
export async function bindEmail(
  email: string,
  code: string,
  proofToken?: string,
  oldCode?: string
): Promise<ApiResponse> {
  const res = await api.post(
    '/api/oauth/email/bind',
    { email, code, old_code: oldCode || undefined },
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      headers: securityProofHeaders(proofToken),
    }
  )
  return res.data
}

/**
 * Send a confirmation code to the current email before replacing it.
 */
export async function sendOldEmailCode(): Promise<ApiResponse> {
  const res = await api.post(
    '/api/oauth/email/bind/old_code',
    undefined,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Bind WeChat account
 */
export async function bindWeChat(
  code: string,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.post(
    '/api/oauth/wechat/bind',
    { code },
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      headers: securityProofHeaders(proofToken),
    }
  )
  return res.data
}

/**
 * Start a Telegram binding flow; the returned one-time token must be
 * carried by the Telegram widget callback URL.
 */
export async function startTelegramBind(): Promise<ApiResponse<string>> {
  const res = await api.post(
    '/api/oauth/telegram/bind/start',
    {},
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// ============================================================================
// Custom OAuth Binding APIs
// ============================================================================

/**
 * Get current user's custom OAuth bindings
 */
export async function getSelfOAuthBindings(): Promise<
  ApiResponse<CustomOAuthBinding[]>
> {
  const res = await api.get(
    '/api/user/oauth/bindings',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

/**
 * Unbind a custom OAuth provider for current user
 */
export async function unbindCustomOAuth(
  providerId: number,
  proofToken?: string
): Promise<ApiResponse> {
  const res = await api.delete(`/api/user/oauth/bindings/${providerId}`, {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    headers: securityProofHeaders(proofToken),
  })
  return res.data
}

// ============================================================================
// Checkin APIs
// ============================================================================

/**
 * Get checkin status for a specific month
 */
export async function getCheckinStatus(
  month: string
): Promise<ApiResponse<CheckinStatusResponse>> {
  const res = await api.get(`/api/user/checkin?month=${month}`)
  return res.data
}

/**
 * Perform daily checkin
 */
export async function performCheckin(): Promise<ApiResponse<CheckinResponse>> {
  const res = await api.post(
    '/api/user/checkin',
    undefined,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}
