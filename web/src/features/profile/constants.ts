// ============================================================================
// Profile Constants
// ============================================================================

/**
 * Default quota warning threshold (500,000 = $1)
 */
export const DEFAULT_QUOTA_WARNING_THRESHOLD = 500000

/**
 * eflow: default API key warning percentage, matches
 * dto.DefaultTokenQuotaWarningPercent
 */
export const DEFAULT_TOKEN_QUOTA_WARNING_PERCENT = 5

/**
 * Notification methods
 */
export const NOTIFICATION_METHODS = [
  { value: 'email' as const, label: 'Email' },
  { value: 'webhook' as const, label: 'Webhook' },
  { value: 'bark' as const, label: 'Bark' },
  { value: 'gotify' as const, label: 'Gotify' },
] as const
