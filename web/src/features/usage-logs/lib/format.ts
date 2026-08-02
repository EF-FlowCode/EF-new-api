import type { StatusBadgeProps } from '@/components/status-badge'
import {
  BILLING_PRICING_VARS,
  normalizeTierLabel,
  parseTiersFromExpr,
  type ParsedTier,
} from '@/features/pricing/lib/billing-expr'
import type { UsageLog } from '../data/schema'
import type { LogOtherData } from '../types'

export { normalizeTierLabel }

const PARAM_OVERRIDE_ACTION_MAP: Record<string, string> = {
  set: 'Set',
  delete: 'Delete',
  copy: 'Copy',
  move: 'Move',
  append: 'Append',
  prepend: 'Prepend',
  trim_prefix: 'Trim Prefix',
  trim_suffix: 'Trim Suffix',
  ensure_prefix: 'Ensure Prefix',
  ensure_suffix: 'Ensure Suffix',
  trim_space: 'Trim Space',
  to_lower: 'To Lower',
  to_upper: 'To Upper',
  replace: 'Replace',
  regex_replace: 'Regex Replace',
  set_header: 'Set Header',
  delete_header: 'Delete Header',
  copy_header: 'Copy Header',
  move_header: 'Move Header',
  pass_headers: 'Pass Headers',
  sync_fields: 'Sync Fields',
  return_error: 'Return Error',
}

/**
 * Get localized label for a param override action
 */
export function getParamOverrideActionLabel(
    action: string,
    t: (key: string) => string
): string {
  const key = PARAM_OVERRIDE_ACTION_MAP[action.toLowerCase()]
  return key ? t(key) : action
}

/**
 * Parse a param override audit line into action and content
 */
export function parseAuditLine(
    line: string
): { action: string; content: string } | null {
  if (typeof line !== 'string') return null
  const firstSpace = line.indexOf(' ')
  if (firstSpace <= 0) return { action: line, content: line }
  return {
    action: line.slice(0, firstSpace),
    content: line.slice(firstSpace + 1),
  }
}

/**
 * Check if the log is a violation fee log
 */
export function isViolationFeeLog(other: LogOtherData | null): boolean {
  if (!other) return false
  return (
      other.violation_fee === true ||
      Boolean(other.violation_fee_code) ||
      Boolean(other.violation_fee_marker)
  )
}

/**
 * Parse the 'other' field from JSON string to object
 */
export function parseLogOther(other: string): LogOtherData | null {
  if (!other) return null
  try {
    return JSON.parse(other) as LogOtherData
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to parse log other field:', error)
    return null
  }
}

/**
 * Get time color based on duration (in seconds)
 */
export function getTimeColor(
    seconds: number
): 'success' | 'warning' | 'danger' {
  if (seconds < 10) return 'success'
  if (seconds < 30) return 'warning'
  return 'danger'
}

/**
 * Get first-response-token color based on latency (in seconds)
 */
export function getFirstResponseTimeColor(
    seconds: number
): 'success' | 'warning' | 'danger' {
  if (seconds < 5) return 'success'
  if (seconds < 10) return 'warning'
  return 'danger'
}

/**
 * Get throughput color based on generated tokens per second
 */
export function getThroughputColor(
    tokensPerSecond: number
): 'success' | 'warning' | 'danger' {
  if (tokensPerSecond >= 30) return 'success'
  if (tokensPerSecond >= 15) return 'warning'
  return 'danger'
}

/**
 * Get response color using throughput only when enough output tokens exist.
 */
export function getResponseTimeColor(
    seconds: number,
    completionTokens: number
): 'success' | 'warning' | 'danger' {
  if (completionTokens < 100 || seconds <= 0) return getTimeColor(seconds)
  return getThroughputColor(completionTokens / seconds)
}

/**
 * Format model name with mapping indicator
 */
export function formatModelName(log: UsageLog): {
  name: string
  isMapped: boolean
  actualModel?: string
} {
  const other = parseLogOther(log.other)
  const isMapped = !!(
      other?.is_model_mapped &&
      other?.upstream_model_name &&
      other.upstream_model_name !== ''
  )

  return {
    name: log.model_name,
    isMapped,
    actualModel: isMapped ? other.upstream_model_name : undefined,
  }
}

/**
 * Decode a base64-encoded billing expression. Safely returns an empty string
 * when the input is missing or malformed (e.g. legacy logs without expr_b64).
 */
export function decodeBillingExprB64(exprB64: string | undefined): string {
  if (!exprB64) return ''
  try {
    const binaryString =
        typeof window !== 'undefined'
            ? window.atob(exprB64)
            : Buffer.from(exprB64, 'base64').toString('binary')
    const bytes = new Uint8Array(binaryString.length)

    for (let i = 0; i < binaryString.length; i++) {
      bytes[i] = binaryString.charCodeAt(i)
    }

    if (typeof TextDecoder !== 'undefined') {
      return new TextDecoder().decode(bytes)
    }

    return decodeURIComponent(
        Array.prototype.map
            .call(bytes, (byte: number) => '%' + byte.toString(16).padStart(2, '0'))
            .join('')
    )
  } catch {
    return ''
  }
}

/**
 * Resolve which parsed tier corresponds to the matched_tier label in a log
 * entry. Missing or unknown labels do not fall back to another tier because
 * that would display guessed unit prices.
 */
export function resolveMatchedTier(
    tiers: ParsedTier[],
    matchedLabel: string | undefined
): ParsedTier | null {
  if (tiers.length === 0) return null
  if (!matchedLabel) return null
  const found = tiers.find((tier) => {
    const l1 = normalizeTierLabel(tier.label)
    const l2 = normalizeTierLabel(matchedLabel)
    return l1 === l2 && l1 !== ''
  })
  return found || null
}

/**
 * Tiered pricing summary derived from an `other` log payload using the
 * billing-expression library. Returns null when the entry is not a tiered
 * billing log or the expression failed to parse.
 */
export interface TieredBillingSummary {
  tiers: ParsedTier[]
  tier: ParsedTier
  priceEntries: Array<{ field: string; shortLabel: string; price: number }>
}

/**
 * Whether the request payload reports any cache-related token usage. Used to
 * suppress cache pricing rows from the tiered breakdown when the request did
 * not exercise the cache path.
 */
export function hasAnyCacheTokens(
    other: LogOtherData | null | undefined
): boolean {
  if (!other) return false
  return (
      (other.cache_tokens || 0) > 0 ||
      (other.cache_creation_tokens || 0) > 0 ||
      (other.cache_creation_tokens_5m || 0) > 0 ||
      (other.cache_creation_tokens_1h || 0) > 0
  )
}

export function getTieredBillingSummary(
    other: LogOtherData | null
): TieredBillingSummary | null {
  if (!other || other.billing_mode !== 'tiered_expr') return null
  const exprStr = decodeBillingExprB64(other.expr_b64)
  if (!exprStr) return null
  const tiers = parseTiersFromExpr(exprStr)
  const tier = resolveMatchedTier(tiers, other.matched_tier)
  if (!tier) return null

  const cacheTokensPresent = hasAnyCacheTokens(other)

  const priceEntries: TieredBillingSummary['priceEntries'] = []
  for (const v of BILLING_PRICING_VARS) {
    if (!v.field) continue
    if (v.group === 'cache' && !cacheTokensPresent) continue
    const raw = tier[v.field as keyof ParsedTier]
    const price = Number(raw)
    if (Number.isFinite(price) && price > 0) {
      priceEntries.push({
        field: v.field,
        shortLabel: v.shortLabel,
        price,
      })
    }
  }
  return { tiers, tier, priceEntries }
}

/**
 * Calculate duration and return formatted result with color variant
 * @param submitTime - Submit timestamp
 * @param finishTime - Finish timestamp
 * @param unit - Unit of the timestamps ('seconds' or 'milliseconds')
 */
export function formatDuration(
    submitTime?: number,
    finishTime?: number,
    unit: 'seconds' | 'milliseconds' = 'milliseconds'
): { durationSec: number; variant: StatusBadgeProps['variant'] } | null {
  if (!submitTime || !finishTime) return null

  const durationSec =
      unit === 'milliseconds'
          ? (finishTime - submitTime) / 1000
          : finishTime - submitTime

  return { durationSec, variant: durationSec > 60 ? 'red' : 'green' }
}

// ============================================================================
// Billing formula breakdown (eflow)
//
// Reconstructs the per-variable cost breakdown shown in the log details
// dialog. All math is done client-side from fields the backend already
// writes into `other` — we DO NOT re-evaluate the billing expression. We
// only multiply already-known per-million-token unit prices by the
// already-known token counts. The matched_tier locks the tier in
// tiered_expr mode, so unit prices are deterministic.
// ============================================================================

/**
 * One term of the cost formula, e.g. `cr × $0.6/M`.
 *
 * varName is the symbol used in the expression (p / c / cr / cc / cc1h /
 * img / img_o / ai / ao). pricePerMillionUSD is the unit price in USD per
 * 1,000,000 tokens. productUSD is `tokens * pricePerMillionUSD / 1e6`.
 */
export interface BillingFormulaTerm {
  varName: string
  varLabel: string // i18n key, e.g. 'Input', 'Output', 'Cache Read'
  tokens: number
  pricePerMillionUSD: number
  productUSD: number
}

export interface BillingFormula {
  terms: BillingFormulaTerm[]
  subtotalUSD: number
  groupRatio: number | null
  groupRatioIsUserExclusive: boolean
  totalUSD: number
  /**
   * cr / (p + cr + cc) — fraction in [0, 1]. null when there is no cache
   * activity (denominator would be zero).
   */
  cacheHitRate: number | null
}

/**
 * Per-variable token counts pulled from a log entry.
 *
 * `p` follows the same auto-exclusion rules as the backend:
 *   - GPT-style usage: prompt_tokens already includes sub-categories, so
 *     subtract whichever sub-categories are separately priced.
 *   - Claude usage (other.claude === true): prompt_tokens is text-only
 *     already; sub-categories are reported separately.
 */
function extractTokensByVar(
  log: UsageLog,
  other: LogOtherData
): Record<string, number> {
  const isClaude = other.claude === true
  const cacheRead = other.cache_tokens || 0
  const cache5m =
    other.cache_creation_tokens_5m || other.cache_creation_tokens || 0
  const cache1h = other.cache_creation_tokens_1h || 0
  // Only audio_input / audio_output appear in LogOtherData — image input
  // tokens are not surfaced in the schema today, so we treat them as 0
  // (the corresponding term is skipped because tokens === 0).
  const audioIn = other.audio_input || 0
  const audioOut = other.audio_output || 0
  const imageOut = other.image_output || 0

  let p = log.prompt_tokens
  let c = log.completion_tokens
  if (!isClaude) {
    // GPT-style totals include cache + media; subtract whatever is
    // separately priced in the formula below.
    p = Math.max(0, p - cacheRead - cache5m - cache1h - audioIn)
    c = Math.max(0, c - audioOut - imageOut)
  }

  return {
    p,
    c,
    cr: cacheRead,
    cc: cache5m,
    cc1h: cache1h,
    img: 0,
    img_o: imageOut,
    ai: audioIn,
    ao: audioOut,
  }
}

/**
 * Per-variable unit prices in USD per 1M tokens.
 *
 * tiered_expr path reads the prices directly from the matched ParsedTier;
 * standard ModelRatio path reconstructs them from `model_ratio × 2` and
 * the per-category sub-ratios — exact same formula as BillingBreakdown
 * uses to render the `$X/M` rows.
 */
function extractPricesByVar(other: LogOtherData): Record<string, number> {
  const isTieredExpr = other.billing_mode === 'tiered_expr'

  if (isTieredExpr) {
    const summary = getTieredBillingSummary(other)
    if (!summary) return {}
    // priceEntries is keyed by ParsedTier.field name (inputPrice,
    // outputPrice, …); look up the matching expression-variable key via
    // BILLING_PRICING_VARS.
    const fieldToKey: Record<string, string> = {}
    for (const v of BILLING_PRICING_VARS) {
      if (v.field) fieldToKey[v.field] = v.key
    }
    const out: Record<string, number> = {}
    for (const entry of summary.priceEntries) {
      const key = fieldToKey[entry.field]
      if (key) out[key] = entry.price
    }
    return out
  }

  if (other.model_ratio == null) return {}
  const baseInputUSD = other.model_ratio * 2.0

  // Mirrors the rendering rules at details-dialog.tsx:198-282 so the
  // formula's terms match what the user already sees in the unit-price
  // rows above it.
  const out: Record<string, number> = {
    p: baseInputUSD,
  }
  if (other.completion_ratio != null) {
    out.c = baseInputUSD * other.completion_ratio
  }
  // Cache prices only when ratio !== 1 (matches BillingBreakdown gating).
  if (other.cache_ratio != null && other.cache_ratio !== 1) {
    out.cr = baseInputUSD * other.cache_ratio
  }
  if (
    other.cache_creation_ratio_5m != null &&
    other.cache_creation_ratio_5m !== 0
  ) {
    out.cc = baseInputUSD * other.cache_creation_ratio_5m
  } else if (
    other.cache_creation_ratio != null &&
    other.cache_creation_ratio !== 1
  ) {
    out.cc = baseInputUSD * other.cache_creation_ratio
  }
  if (
    other.cache_creation_ratio_1h != null &&
    other.cache_creation_ratio_1h !== 0
  ) {
    out.cc1h = baseInputUSD * other.cache_creation_ratio_1h
  }
  if (other.audio_ratio != null && other.audio_ratio !== 1) {
    out.ai = baseInputUSD * other.audio_ratio
  }
  if (
    other.audio_completion_ratio != null &&
    other.audio_completion_ratio !== 1
  ) {
    out.ao = baseInputUSD * other.audio_completion_ratio
  }
  if (other.image_ratio != null && other.image_ratio !== 1) {
    out.img = baseInputUSD * other.image_ratio
  }
  return out
}

/**
 * Build a structured cost breakdown for the log entry. Returns null for
 * paths where the additive formula does not apply (per-call, audio_seconds,
 * duration_seconds) or when the billing data is incomplete.
 */
export function buildBillingFormula(
  log: UsageLog,
  other: LogOtherData
): BillingFormula | null {
  const billingMode = other.billing_mode
  const isTieredExpr = billingMode === 'tiered_expr'

  // Skip non-additive billing modes: per-call / audio_seconds /
  // duration_seconds all bypass the (Σ tokens × price) shape.
  if (
    billingMode === 'audio_seconds' ||
    billingMode === 'duration_seconds'
  ) {
    return null
  }
  if (!isTieredExpr && (other.model_price ?? 0) > 0) {
    return null
  }
  if (!isTieredExpr && other.model_ratio == null) {
    return null
  }

  const tokensByVar = extractTokensByVar(log, other)
  const pricesByVar = extractPricesByVar(other)

  if (Object.keys(pricesByVar).length === 0) return null

  const labelByVar: Record<string, string> = {}
  for (const v of BILLING_PRICING_VARS) {
    labelByVar[v.key] = v.shortLabel
  }

  const terms: BillingFormulaTerm[] = []
  // Iterate in BILLING_PRICING_VARS order so display order is stable
  // (input, output, cache read, cache write 5m, cache write 1h, …).
  for (const v of BILLING_PRICING_VARS) {
    const key = v.key
    const tokens = tokensByVar[key] ?? 0
    const price = pricesByVar[key] ?? 0
    if (tokens <= 0 || price <= 0) continue
    terms.push({
      varName: key,
      varLabel: labelByVar[key] ?? key,
      tokens,
      pricePerMillionUSD: price,
      productUSD: (tokens * price) / 1_000_000,
    })
  }

  if (terms.length === 0) return null

  const subtotalUSD = terms.reduce((sum, term) => sum + term.productUSD, 0)

  const userGR = other.user_group_ratio
  const isUserExclusive =
    userGR != null && Number.isFinite(userGR) && userGR !== -1
  const effectiveGR = isUserExclusive ? userGR : other.group_ratio
  const groupRatio =
    effectiveGR != null && Number.isFinite(effectiveGR) ? effectiveGR : null
  const totalUSD = subtotalUSD * (groupRatio ?? 1)

  // Cache hit rate: cr / (p + cr + cc), excludes cc1h per user spec.
  const pTokens = tokensByVar.p
  const crTokens = tokensByVar.cr
  const ccTokens = tokensByVar.cc
  const cacheDenom = pTokens + crTokens + ccTokens
  const cacheHitRate = cacheDenom > 0 && crTokens > 0 ? crTokens / cacheDenom : null

  return {
    terms,
    subtotalUSD,
    groupRatio,
    groupRatioIsUserExclusive: isUserExclusive,
    totalUSD,
    cacheHitRate,
  }
}
