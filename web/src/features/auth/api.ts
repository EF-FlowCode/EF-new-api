import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import type {
  LoginPayload,
  LoginResponse,
  Login2FAResponse,
  TwoFAPayload,
  RegisterPayload,
  ApiResponse,
} from './types'

// ============================================================================
// Authentication APIs
// ============================================================================

// ----------------------------------------------------------------------------
// Login & Logout
// ----------------------------------------------------------------------------

// User login with username and password
export async function login(payload: LoginPayload) {
  const res = await api.post<LoginResponse>('/api/user/login', payload)
  return res.data
}

// Two-factor authentication login
export async function login2fa(payload: TwoFAPayload) {
  const res = await api.post<Login2FAResponse>(
    '/api/user/login/2fa',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Complete a pending login with a TOTP or backup code (flow-bound).
export async function verifyLogin(flowToken: string, code: string) {
  const res = await api.post<Login2FAResponse>(
    '/api/user/login/verify',
    { flow_token: flowToken, method: '2fa', code },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Start a Passkey assertion for a pending login.
export async function beginLoginPasskey(flowToken: string) {
  const res = await api.post<{
    success: boolean
    message?: string
    data?: { flow_token: string; options: unknown }
  }>(
    '/api/user/login/passkey/begin',
    { flow_token: flowToken },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// Finish the Passkey assertion and sign in.
export async function finishLoginPasskey(
  flowToken: string,
  passkeyFlowToken: string,
  credential: Record<string, unknown>
) {
  const res = await api.post<Login2FAResponse>(
    '/api/user/login/passkey/finish',
    { flow_token: flowToken, passkey_flow_token: passkeyFlowToken, credential },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return res.data
}

// User logout
export async function logout(): Promise<ApiResponse> {
  const res = await api.get('/api/user/logout')
  return res.data
}

// ----------------------------------------------------------------------------
// Password Management
// ----------------------------------------------------------------------------

// Send password reset email
export async function sendPasswordResetEmail(
  email: string
): Promise<ApiResponse> {
  const res = await api.get('/api/reset_password', {
    params: { email },
  })
  return res.data
}

// ----------------------------------------------------------------------------
// OAuth
// ----------------------------------------------------------------------------

// Telegram signs in with OpenID Connect + PKCE: the server keeps the verifier
// and returns the authorization URL to open.
export async function getTelegramAuthorizationURL(
  invitationCode?: string
): Promise<string> {
  const aff =
    typeof window !== 'undefined' ? (localStorage.getItem('aff') ?? '') : ''
  const res = await api.get('/api/oauth/state', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params: {
      aff,
      invitation_code: invitationCode?.trim() || undefined,
      provider: 'telegram',
    },
  })
  return res.data?.success ? (res.data.authorization_url ?? '') : ''
}

// Get OAuth state for CSRF protection. The invitation code entered on the
// sign-up page is stored in the same session for OAuth registration.
export async function getOAuthState(invitationCode?: string): Promise<string> {
  const aff =
    typeof window !== 'undefined' ? (localStorage.getItem('aff') ?? '') : ''
  const invitation_code = invitationCode?.trim() || undefined
  const res = await api.get('/api/oauth/state', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params: { aff, invitation_code },
  })
  if (res.data?.success) return res.data.data
  return ''
}

// WeChat login by authorization code
export async function wechatLoginByCode(
  code: string,
  invitationCode?: string
): Promise<LoginResponse> {
  const invitation_code = invitationCode?.trim() || undefined
  const res = await api.get('/api/oauth/wechat', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params: { code, invitation_code },
  })
  return res.data
}

// ----------------------------------------------------------------------------
// Registration
// ----------------------------------------------------------------------------

// User registration
export async function register(payload: RegisterPayload): Promise<ApiResponse> {
  const res = await api.post(`/api/user/register`, payload, {
    params: { turnstile: payload.turnstile ?? '' },
  })
  return res.data
}

// Send email verification code
export async function sendEmailVerification(
  email: string,
  turnstile?: string
): Promise<ApiResponse> {
  const res = await api.get('/api/verification', {
    params: { email, turnstile },
  })
  return res.data
}

// Bind email to OAuth account
export async function bindEmail(
  email: string,
  code: string
): Promise<ApiResponse> {
  const res = await api.post('/api/oauth/email/bind', {
    email,
    code,
  })
  return res.data
}
