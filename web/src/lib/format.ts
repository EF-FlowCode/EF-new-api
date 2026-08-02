import dayjs from '@/lib/dayjs'
import {
  formatCurrencyFromUSD,
  formatQuotaWithCurrency,
  getCurrencyDisplay,
} from './currency'

// ============================================================================
// Number Formatting
// ============================================================================

export function formatNumber(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value as number)) return '-'
  return Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(
    value as number
  )
}

export function formatCompactNumber(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value as number)) return '-'
  return Intl.NumberFormat(undefined, {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value as number)
}

export function formatPercent(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value as number)) return '-'
  return Intl.NumberFormat(undefined, {
    style: 'percent',
    maximumFractionDigits: 2,
  }).format((value as number) / 100)
}

export function formatCurrencyUSD(value: number | null | undefined): string {
  return formatCurrencyFromUSD(value == null ? null : (value as number))
}

// ============================================================================
// Quota Formatting (500,000 units = $1)
// ============================================================================

/**
 * Format quota into the configured display amount.
 * Quota is stored in units where `quotaPerUnit` equals 1 USD.
 */
export function formatQuota(quota: number): string {
  return formatQuotaWithCurrency(quota, {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: true,
  })
}

/**
 * Parse quota from the current display input back to quota units.
 */
export function parseQuotaFromDollars(amount: number): number {
  if (!Number.isFinite(amount)) return 0

  const { config, meta } = getCurrencyDisplay()

  // Tokens-only or raw quota mode
  if (meta.kind === 'tokens') {
    return Math.round(amount)
  }

  const exchangeRate =
    meta.kind === 'currency' || meta.kind === 'custom' ? meta.exchangeRate : 1

  const usdAmount = exchangeRate > 0 ? amount / exchangeRate : amount

  return Math.round(usdAmount * config.quotaPerUnit)
}

/**
 * Convert quota units to the configured display amount.
 * Reverse of parseQuotaFromDollars.
 */
export function quotaUnitsToDollars(units: number): number {
  const { config, meta } = getCurrencyDisplay()

  if (meta.kind === 'tokens') {
    return units
  }

  const usdAmount = units / config.quotaPerUnit
  const exchangeRate =
    meta.kind === 'currency' || meta.kind === 'custom' ? meta.exchangeRate : 1

  return usdAmount * exchangeRate
}

// ============================================================================
// Timestamp Formatting
// ============================================================================

// eflow: 时间戳一律按北京时间展示，不随访问者浏览器/机器时区漂移。
// 起因：宿主机/容器/访问机时区不一致（host BST、容器 CST、有人浏览器 UTC），
// 日志页 dayjs(ms).format() 跟浏览器本地时区走 → 同一条"现在"的日志在 UTC 机器上
// 显示成 8 小时前，被误判成"日志停了/不见了"。固定 Asia/Shanghai 后任何机器看都是北京时间。
const DISPLAY_TZ = 'Asia/Shanghai'

/**
 * Format Unix timestamp (seconds) to YYYY-MM-DD HH:mm:ss
 */
export function formatTimestamp(timestamp: number): string {
  if (timestamp === -1) {
    return 'Never'
  }
  return formatTimestampToDate(timestamp)
}

/**
 * Format timestamp to YYYY-MM-DD HH:mm:ss（固定北京时间，见 DISPLAY_TZ）
 * @param timestamp - Timestamp in seconds or milliseconds
 * @param unit - Unit of the timestamp ('seconds' or 'milliseconds')
 */
export function formatTimestampToDate(
  timestamp?: number,
  unit: 'seconds' | 'milliseconds' = 'seconds'
): string {
  if (!timestamp || timestamp === -1 || timestamp === 0) {
    return '-'
  }
  const ms = unit === 'seconds' ? timestamp * 1000 : timestamp
  return dayjs(ms).tz(DISPLAY_TZ).format('YYYY-MM-DD HH:mm:ss')
}

/** Format a Date object to YYYY-MM-DD HH:mm:ss */
export function formatDateTimeStr(date: Date): string {
  return dayjs(date).format('YYYY-MM-DD HH:mm:ss')
}

/** Format a Date object to YYYY-MM-DD */
export function formatDateStr(date: Date): string {
  return dayjs(date).format('YYYY-MM-DD')
}

/** Format a Date object to HH:mm:ss */
export function formatTimeStr(date: Date): string {
  return dayjs(date).format('HH:mm:ss')
}

/**
 * Format quota for usage logs with higher precision
 * Uses 6 decimal places to show very small costs accurately
 */
export function formatLogQuota(quota: number): string {
  return formatQuotaWithCurrency(quota, {
    digitsLarge: 4,
    digitsSmall: 6,
    abbreviate: false,
  })
}

/**
 * Format tokens count with K/M suffixes
 */
export function formatTokens(tokens: number): string {
  if (tokens === 0) return '-'
  if (tokens < 1000) return tokens.toString()
  if (tokens < 1000000) return `${(tokens / 1000).toFixed(1)}K`
  return `${(tokens / 1000000).toFixed(2)}M`
}

/**
 * Format use time in seconds with appropriate unit
 */
export function formatUseTime(seconds: number): string {
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  return `${minutes}m ${remainingSeconds.toFixed(0)}s`
}

/**
 * Format timestamp to date input value (YYYY-MM-DDTHH:mm)
 */
export function formatTimestampForInput(timestamp: number): string {
  if (timestamp === -1) {
    return ''
  }
  return dayjs(timestamp * 1000).format('YYYY-MM-DDTHH:mm')
}

/**
 * Parse datetime-local input to Unix timestamp
 */
export function parseTimestampFromInput(value: string): number {
  if (!value) {
    return -1
  }
  const date = new Date(value)
  return Math.floor(date.getTime() / 1000)
}

// ============================================================================
// Color Generation
// ============================================================================

/**
 * Generate a consistent color from a string
 * Uses HSL for better color distribution
 */
export function stringToColor(str: string): string {
  if (!str) return 'gray'

  // Generate hash from string
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash)
    hash = hash & hash // Convert to 32-bit integer
  }

  // Use hash to generate hue (0-360)
  const hue = Math.abs(hash % 360)

  // Use saturation and lightness that work well for tags
  const saturation = 65 + (Math.abs(hash) % 10) // 65-75%
  const lightness = 55 + (Math.abs(hash >> 8) % 10) // 55-65%

  return `hsl(${hue}, ${saturation}%, ${lightness}%)`
}
