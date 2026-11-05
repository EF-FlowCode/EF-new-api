// eflow: API key quota warning line, same rules as service.tokenQuotaWarningLine.

export const DEFAULT_TOKEN_QUOTA_WARNING_PERCENT = 5

export type TokenQuotaWarningSettings = {
  token_quota_warning_disabled?: boolean
  token_quota_warning_percent?: number
  token_quota_warning_amount?: number
}

/** Reads the warning settings from the user's setting field (JSON or object). */
export function parseTokenQuotaWarningSettings(
  setting: Record<string, unknown> | string | undefined
): TokenQuotaWarningSettings {
  if (!setting) return {}
  if (typeof setting === 'string') {
    try {
      return JSON.parse(setting) as TokenQuotaWarningSettings
    } catch {
      return {}
    }
  }
  return setting as TokenQuotaWarningSettings
}

/**
 * Remaining quota below which a limited key warns: the higher of the
 * percentage of its total and the fixed amount. Null when the warning is off.
 */
export function tokenQuotaWarningLine(
  settings: TokenQuotaWarningSettings,
  total: number
): number | null {
  if (settings.token_quota_warning_disabled) return null
  const percent =
    settings.token_quota_warning_percent ?? DEFAULT_TOKEN_QUOTA_WARNING_PERCENT
  let line = percent > 0 && total > 0 ? Math.floor((total * percent) / 100) : 0
  const amount = settings.token_quota_warning_amount ?? 0
  if (amount > line) line = amount
  return line > 0 ? line : null
}

export function isTokenBelowWarningLine(
  settings: TokenQuotaWarningSettings,
  remaining: number,
  used: number
): boolean {
  const line = tokenQuotaWarningLine(settings, remaining + used)
  return line !== null && remaining < line
}
