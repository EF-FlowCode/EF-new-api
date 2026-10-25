/**
 * A pending login waiting for its second factor (backend LoginChallenge).
 * Older servers only send require_2fa; newer ones also send the flow token and
 * the allowed methods (TOTP and/or Passkey).
 */
export interface LoginChallenge {
  require_verification?: boolean
  require_2fa?: boolean
  flow_token?: string
  expires_at?: number
  methods?: { method: string; available: boolean; reason?: string }[]
}

const STORAGE_KEY = 'login_challenge'

export function isLoginChallenge(data: unknown): boolean {
  if (!data || typeof data !== 'object') return false
  const challenge = data as LoginChallenge
  return Boolean(challenge.require_verification || challenge.require_2fa)
}

/** Keep the challenge for the verification page (this tab only). */
export function rememberLoginChallenge(challenge: unknown) {
  try {
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(challenge))
  } catch {
    // Storage may be unavailable; the page falls back to the TOTP form.
  }
}

export function readLoginChallenge(): LoginChallenge | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const challenge = JSON.parse(raw) as LoginChallenge
    if (challenge.expires_at && challenge.expires_at * 1000 < Date.now()) {
      sessionStorage.removeItem(STORAGE_KEY)
      return null
    }
    return challenge
  } catch {
    return null
  }
}

export function clearLoginChallenge() {
  try {
    sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

/** Which second factors the verification page should offer. */
export function challengeMethods(challenge: LoginChallenge | null) {
  const methods = challenge?.methods
  if (!methods || methods.length === 0) {
    return { twoFA: true, passkey: false }
  }
  return {
    twoFA: methods.some((m) => m.method === '2fa' && m.available),
    passkey: methods.some((m) => m.method === 'passkey' && m.available),
  }
}
