import {
  getOAuthSessionStorage,
  markOAuthBindPopup,
} from '@/features/auth/lib/oauth-callback-mode'
import { api } from './api'

// ============================================================================
// OAuth URL Builders
// ============================================================================

/**
 * Custom OAuth binding returned by the user binding endpoints.
 *
 * Providers are identified by their numeric database id and the linked
 * account is exposed as `provider_user_id`. Keep this contract shared by the
 * profile and administrator views.
 */
export interface CustomOAuthBinding {
  provider_id: number
  provider_name: string
  provider_slug: string
  provider_icon: string
  provider_user_id: string
}

export interface CustomOAuthBindingProvider {
  slug: string
  client_id: string
  authorization_endpoint: string
  scopes?: string
}

export function indexCustomOAuthBindings(
  bindings: readonly CustomOAuthBinding[]
): Map<number, CustomOAuthBinding> {
  return new Map(bindings.map((binding) => [binding.provider_id, binding]))
}

export function buildCustomOAuthAuthorizationUrl(
  origin: string,
  providerSlug: string,
  authorizationEndpoint: string,
  clientId: string,
  scopes: string | undefined,
  state: string
): string {
  const url = new URL(authorizationEndpoint)
  const normalizedOrigin = origin.replace(/\/+$/, '')
  const encodedSlug = encodeURIComponent(providerSlug)
  url.searchParams.set('client_id', clientId)
  url.searchParams.set(
    'redirect_uri',
    `${normalizedOrigin}/oauth/${encodedSlug}`
  )
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('state', state)
  if (scopes) url.searchParams.set('scope', scopes)
  return url.toString()
}

/**
 * Build GitHub OAuth URL
 */
export function buildGitHubOAuthUrl(clientId: string, state: string): string {
  return `https://github.com/login/oauth/authorize?client_id=${clientId}&state=${state}&scope=user:email`
}

/**
 * Build Discord OAuth URL
 */
export function buildDiscordOAuthUrl(clientId: string, state: string): string {
  const url = new URL('https://discord.com/oauth2/authorize')
  url.searchParams.set('client_id', clientId)
  url.searchParams.set(
    'redirect_uri',
    `${window.location.origin}/oauth/discord`
  )
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', 'identify+openid')
  url.searchParams.set('state', state)
  return url.toString()
}

/**
 * Build OIDC OAuth URL
 */
export function buildOIDCOAuthUrl(
  authUrl: string,
  clientId: string,
  state: string
): string {
  const url = new URL(authUrl)
  url.searchParams.set('client_id', clientId)
  url.searchParams.set('redirect_uri', `${window.location.origin}/oauth/oidc`)
  url.searchParams.set('response_type', 'code')
  url.searchParams.set('scope', 'openid profile email')
  url.searchParams.set('state', state)
  return url.toString()
}

/**
 * Build LinuxDO OAuth URL
 */
export function buildLinuxDOOAuthUrl(clientId: string, state: string): string {
  return `https://connect.linux.do/oauth2/authorize?response_type=code&client_id=${clientId}&state=${state}`
}

// ============================================================================
// OAuth Helper Functions
// ============================================================================

/**
 * Get OAuth state token
 * Includes affiliate code from localStorage if available
 */
export async function getOAuthState(): Promise<string | null> {
  try {
    let path = '/api/oauth/state'
    const affCode = localStorage.getItem('aff')
    if (affCode && affCode.length > 0) {
      path += `?aff=${affCode}`
    }
    const res = await api.get(path)
    if (res.data.success) {
      return res.data.data
    }
    return null
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to get OAuth state:', error)
    return null
  }
}

/**
 * Obtains the account.binding.bind proof for a provider; resolves null when the
 * user dismisses verification. Registered by the account bindings page.
 */
type BindProofProvider = (provider: string) => Promise<string | null>
let bindProofProvider: BindProofProvider | null = null

export function setOAuthBindProofProvider(provider: BindProofProvider | null) {
  bindProofProvider = provider
}

/** OAuth state for binding: consumes the proof bound to this provider. */
async function getOAuthBindState(
  provider: string,
  proofToken: string
): Promise<{ state: string; authorizationURL?: string } | null> {
  try {
    const res = await api.get('/api/oauth/state', {
      params: { purpose: 'bind', provider },
      headers: { 'X-Security-Proof': proofToken },
      skipBusinessError: true,
      skipErrorNotification: true,
    })
    return res.data?.success
      ? { state: res.data.data, authorizationURL: res.data.authorization_url }
      : null
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to get OAuth bind state:', error)
    return null
  }
}

async function openOAuthBinding(
  provider: string,
  buildUrl: (state: string, authorizationURL?: string) => string
): Promise<void> {
  // Open the popup inside the click so it is not blocked; it stays blank
  // while the user confirms their identity.
  const popup = window.open('', '_blank')
  if (!popup) return

  const proof = bindProofProvider ? await bindProofProvider(provider) : null
  if (!proof) {
    popup.close()
    return
  }
  const bind = await getOAuthBindState(provider, proof)
  const state = bind?.state
  if (
    !state ||
    !markOAuthBindPopup(getOAuthSessionStorage(popup), provider, state)
  ) {
    popup.close()
    return
  }
  popup.location.replace(buildUrl(state, bind?.authorizationURL))
}

/**
 * Bind Telegram through OpenID Connect (the server builds the PKCE URL).
 */
export async function handleTelegramOAuthBinding(): Promise<void> {
  await openOAuthBinding(
    'telegram',
    (_state, authorizationURL) => authorizationURL ?? ''
  )
}

/**
 * Handle GitHub OAuth binding/login
 */
export async function handleGitHubOAuth(clientId: string): Promise<void> {
  await openOAuthBinding('github', (state) =>
    buildGitHubOAuthUrl(clientId, state)
  )
}

/**
 * Handle Discord OAuth binding/login
 */
export async function handleDiscordOAuth(clientId: string): Promise<void> {
  await openOAuthBinding('discord', (state) =>
    buildDiscordOAuthUrl(clientId, state)
  )
}

/**
 * Handle OIDC OAuth binding/login
 */
export async function handleOIDCOAuth(
  authUrl: string,
  clientId: string
): Promise<void> {
  await openOAuthBinding('oidc', (state) =>
    buildOIDCOAuthUrl(authUrl, clientId, state)
  )
}

/**
 * Handle LinuxDO OAuth binding/login
 */
export async function handleLinuxDOOAuth(clientId: string): Promise<void> {
  await openOAuthBinding('linuxdo', (state) =>
    buildLinuxDOOAuthUrl(clientId, state)
  )
}

export async function handleCustomOAuthBinding(
  provider: CustomOAuthBindingProvider
): Promise<void> {
  if (
    !provider.slug ||
    !provider.authorization_endpoint ||
    !provider.client_id
  ) {
    return
  }
  await openOAuthBinding(provider.slug, (state) =>
    buildCustomOAuthAuthorizationUrl(
      window.location.origin,
      provider.slug,
      provider.authorization_endpoint,
      provider.client_id,
      provider.scopes,
      state
    )
  )
}
