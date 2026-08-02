import { z } from 'zod'

// ============================================================================
// Channel Rate Limit
// ============================================================================

export const ON_LIMIT_VALUES = ['skip', 'queue', 'reject'] as const
export type OnLimitStrategy = (typeof ON_LIMIT_VALUES)[number]

export const channelRateLimitSchema = z.object({
  enabled: z.boolean(),
  rpm: z.number().int().min(0),
  concurrency: z.number().int().min(0),
  on_limit: z.enum(ON_LIMIT_VALUES),
  queue_max_wait_ms: z.number().int().min(0),
  queue_depth: z.number().int().min(0),
})

export type ChannelRateLimit = z.infer<typeof channelRateLimitSchema>

export const DEFAULT_CHANNEL_RATE_LIMIT: ChannelRateLimit = {
  enabled: false,
  rpm: 0,
  concurrency: 0,
  on_limit: 'skip',
  queue_max_wait_ms: 2000,
  queue_depth: 20,
}

export function normalizeChannelRateLimit(
  value: unknown
): ChannelRateLimit {
  const src =
    value && typeof value === 'object' ? (value as Record<string, unknown>) : {}
  const rpmRaw = Number(src.rpm)
  const concurrencyRaw = Number(src.concurrency)
  const queueMaxWaitRaw = Number(src.queue_max_wait_ms)
  const queueDepthRaw = Number(src.queue_depth)
  const onLimit = ON_LIMIT_VALUES.includes(src.on_limit as OnLimitStrategy)
    ? (src.on_limit as OnLimitStrategy)
    : DEFAULT_CHANNEL_RATE_LIMIT.on_limit
  return {
    enabled: !!src.enabled,
    rpm: Number.isFinite(rpmRaw) ? Math.max(0, Math.floor(rpmRaw)) : 0,
    concurrency: Number.isFinite(concurrencyRaw)
      ? Math.max(0, Math.floor(concurrencyRaw))
      : 0,
    on_limit: onLimit,
    queue_max_wait_ms: Number.isFinite(queueMaxWaitRaw)
      ? Math.max(0, Math.floor(queueMaxWaitRaw))
      : DEFAULT_CHANNEL_RATE_LIMIT.queue_max_wait_ms,
    queue_depth: Number.isFinite(queueDepthRaw)
      ? Math.max(0, Math.floor(queueDepthRaw))
      : DEFAULT_CHANNEL_RATE_LIMIT.queue_depth,
  }
}

// ============================================================================
// Risk Control Headers
// ============================================================================

export const RISK_CONTROL_SOURCES = [
  'username',
  'user_id',
  'user_email',
  'user_group',
  'using_group',
  'token_id',
  'request_id',
  'custom',
] as const
export type RiskControlSource = (typeof RISK_CONTROL_SOURCES)[number]

export const riskControlHeaderRuleSchema = z.object({
  name: z.string(),
  source: z.enum(RISK_CONTROL_SOURCES),
  value: z.string(),
})

export type RiskControlHeaderRule = z.infer<typeof riskControlHeaderRuleSchema>

export function normalizeRiskControlHeaderRule(
  rule: unknown
): RiskControlHeaderRule {
  const src =
    rule && typeof rule === 'object' ? (rule as Record<string, unknown>) : {}
  const source = RISK_CONTROL_SOURCES.includes(src.source as RiskControlSource)
    ? (src.source as RiskControlSource)
    : 'username'
  return {
    name: String(src.name || '').trim(),
    source,
    value: source === 'custom' ? String(src.value || '') : '',
  }
}

export function normalizeRiskControlHeaders(
  rules: unknown
): RiskControlHeaderRule[] {
  if (!Array.isArray(rules)) return []
  return rules
    .map(normalizeRiskControlHeaderRule)
    .filter((rule) => rule.name.length > 0 || rule.value.length > 0)
}

export function createEmptyRiskControlHeaderRule(): RiskControlHeaderRule {
  return { name: '', source: 'username', value: '' }
}

// ============================================================================
// Error Filter Rules
// ============================================================================

export const ERROR_FILTER_ACTIONS = ['retry', 'rewrite', 'replace'] as const
export type ErrorFilterAction = (typeof ERROR_FILTER_ACTIONS)[number]

export const errorFilterRuleSchema = z.object({
  status_codes: z.array(z.number().int()),
  message_contains: z.array(z.string()),
  error_codes: z.array(z.string()),
  action: z.enum(ERROR_FILTER_ACTIONS),
  rewrite_message: z.string(),
  replace_status_code: z.number().int().min(100),
  replace_message: z.string(),
})

export type ErrorFilterRule = z.infer<typeof errorFilterRuleSchema>

const dedupeStrings = (values: unknown): string[] =>
  Array.from(
    new Set(
      (Array.isArray(values) ? values : [])
        .map((v) => String(v ?? '').trim())
        .filter(Boolean)
    )
  )

const dedupeStatusCodes = (values: unknown): number[] =>
  Array.from(
    new Set(
      (Array.isArray(values) ? values : [])
        .map((v) => Number.parseInt(String(v).trim(), 10))
        .filter((v) => Number.isInteger(v) && v >= 100 && v <= 599)
    )
  )

export function normalizeErrorFilterRule(rule: unknown): ErrorFilterRule {
  const src =
    rule && typeof rule === 'object' ? (rule as Record<string, unknown>) : {}
  const action = ERROR_FILTER_ACTIONS.includes(src.action as ErrorFilterAction)
    ? (src.action as ErrorFilterAction)
    : 'retry'
  const replaceStatusRaw = Number.parseInt(String(src.replace_status_code), 10)
  return {
    status_codes: dedupeStatusCodes(src.status_codes),
    message_contains: dedupeStrings(src.message_contains),
    error_codes: dedupeStrings(src.error_codes),
    action,
    rewrite_message: String(src.rewrite_message || ''),
    replace_status_code:
      Number.isInteger(replaceStatusRaw) && replaceStatusRaw >= 100
        ? replaceStatusRaw
        : 200,
    replace_message: String(src.replace_message || ''),
  }
}

export function normalizeErrorFilterRules(
  rules: unknown
): ErrorFilterRule[] {
  if (!Array.isArray(rules)) return []
  return rules.map(normalizeErrorFilterRule)
}

export function createEmptyErrorFilterRule(): ErrorFilterRule {
  return {
    status_codes: [],
    message_contains: [],
    error_codes: [],
    action: 'retry',
    rewrite_message: '',
    replace_status_code: 200,
    replace_message: '',
  }
}
