import type { User } from '@/features/users/types'

// ============================================================================
// API Payloads
// ============================================================================

export interface LoginPayload {
  username: string
  password: string
}

export interface TwoFAPayload {
  code: string
}

export interface RegisterPayload {
  username: string
  password: string
  email?: string
  verification_code?: string
  invitation_code?: string
  aff_code?: string
  turnstile?: string
}

export interface PasswordResetPayload {
  email: string
}

export interface EmailVerificationPayload {
  email: string
  turnstile?: string
}

export interface BindEmailPayload {
  email: string
  code: string
}

// ============================================================================
// API Responses
// ============================================================================

export interface LoginResponse {
  success: boolean
  message: string
  data?: {
    require_2fa?: boolean
    require_verification?: boolean
    flow_token?: string
    expires_at?: number
    methods?: { method: string; available: boolean; reason?: string }[]
    id?: number
  }
}

export interface Login2FAResponse {
  success: boolean
  message: string
  data?: User
}

export interface ApiResponse {
  success: boolean
  message: string
  data?: unknown
}

// ============================================================================
// System Status
// ============================================================================

export interface SystemStatus {
  success?: boolean
  message?: string
  data?: {
    version?: string
    system_name?: string
    logo?: string
    github_oauth?: boolean
    github_client_id?: string
    discord_oauth?: boolean
    discord_client_id?: string
    oidc_enabled?: boolean
    oidc_authorization_endpoint?: string
    oidc_client_id?: string
    oidc_display_name?: string
    linuxdo_oauth?: boolean
    linuxdo_client_id?: string
    telegram_oauth?: boolean
    telegram_oidc?: boolean
    passkey_login?: boolean
    wechat_login?: boolean
    wechat_qrcode?: string
    wechat_qr_code?: string
    wechat_qrcode_image_url?: string
    wechat_qr_code_image_url?: string
    wechat_account_qrcode_image_url?: string
    WeChatAccountQRCodeImageURL?: string
    turnstile_check?: boolean
    turnstile_site_key?: string
    email_verification?: boolean
    invitation_code_enabled?: boolean
    invitation_code_oauth_required?: boolean
    invitation_code_user_generate_enabled?: boolean
    group_monitoring_available?: boolean
    self_use_mode_enabled?: boolean
    display_in_currency?: boolean
    quota_per_unit?: number
    quota_display_type?: string
    usd_exchange_rate?: number
    custom_currency_symbol?: string
    custom_currency_exchange_rate?: number
    demo_site_enabled?: boolean
    user_agreement_enabled?: boolean
    privacy_policy_enabled?: boolean
    oauth_register_enabled?: boolean
    register_enabled?: boolean
    password_register_enabled?: boolean
    custom_oauth_providers?: CustomOAuthProviderInfo[]
    [key: string]: unknown
  }
  // Allow direct access to common properties
  version?: string
  system_name?: string
  logo?: string
  github_oauth?: boolean
  github_client_id?: string
  discord_oauth?: boolean
  discord_client_id?: string
  oidc_enabled?: boolean
  oidc_authorization_endpoint?: string
  oidc_client_id?: string
  oidc_display_name?: string
  linuxdo_oauth?: boolean
  linuxdo_client_id?: string
  telegram_oauth?: boolean
  telegram_oidc?: boolean
  passkey_login?: boolean
  wechat_login?: boolean
  wechat_qrcode?: string
  wechat_qr_code?: string
  wechat_qrcode_image_url?: string
  wechat_qr_code_image_url?: string
  wechat_account_qrcode_image_url?: string
  WeChatAccountQRCodeImageURL?: string
  turnstile_check?: boolean
  turnstile_site_key?: string
  email_verification?: boolean
  invitation_code_enabled?: boolean
  invitation_code_oauth_required?: boolean
  invitation_code_user_generate_enabled?: boolean
  group_monitoring_available?: boolean
  self_use_mode_enabled?: boolean
  display_in_currency?: boolean
  quota_per_unit?: number
  quota_display_type?: string
  usd_exchange_rate?: number
  custom_currency_symbol?: string
  custom_currency_exchange_rate?: number
  demo_site_enabled?: boolean
  user_agreement_enabled?: boolean
  privacy_policy_enabled?: boolean
  oauth_register_enabled?: boolean
  register_enabled?: boolean
  password_register_enabled?: boolean
  custom_oauth_providers?: CustomOAuthProviderInfo[]
  [key: string]: unknown
}

// ============================================================================
// OAuth
// ============================================================================

export interface OAuthProvider {
  name: string
  type: 'github' | 'discord' | 'oidc' | 'linuxdo' | 'telegram' | 'wechat'
  enabled: boolean
  clientId?: string
  authEndpoint?: string
}

export interface CustomOAuthProviderInfo {
  id: number
  name: string
  slug: string
  icon: string
  client_id: string
  authorization_endpoint: string
  scopes: string
}

// ============================================================================
// Form Props
// ============================================================================

export interface AuthFormProps extends React.HTMLAttributes<HTMLFormElement> {
  redirectTo?: string
}
