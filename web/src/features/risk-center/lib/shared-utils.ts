// 拆出来满足 react-refresh/only-export-components——shared.tsx 只导出组件，
// 这边放常量和纯工具函数。

export const RISK_METRIC_DEFINITIONS: Array<{
  label: string
  value: string
  scopes: Array<'token' | 'user' | 'ip'>
}> = [
  {
    label: 'Distinct IPs (10m)',
    value: 'distinct_ip_10m',
    scopes: ['token', 'user'],
  },
  {
    label: 'Distinct IPs (1h)',
    value: 'distinct_ip_1h',
    scopes: ['token', 'user'],
  },
  { label: 'Distinct UAs (10m)', value: 'distinct_ua_10m', scopes: ['token'] },
  {
    label: 'Tokens per IP (10m)',
    value: 'tokens_per_ip_10m',
    scopes: ['token'],
  },
  {
    label: 'Requests (1m)',
    value: 'request_count_1m',
    scopes: ['token', 'user'],
  },
  {
    label: 'Requests (10m)',
    value: 'request_count_10m',
    scopes: ['token', 'user'],
  },
  {
    label: 'In-flight (now)',
    value: 'inflight_now',
    scopes: ['token', 'user'],
  },
  {
    label: 'Rule hits (24h)',
    value: 'rule_hit_count_24h',
    scopes: ['token', 'user'],
  },
  { label: 'Risk score', value: 'risk_score', scopes: ['token', 'user'] },
]

export const RISK_OP_OPTIONS = ['>=', '>', '<=', '<', '==', '!='] as const

export function metricOptionsForScope(scope: 'token' | 'user' | 'ip') {
  return RISK_METRIC_DEFINITIONS.filter((m) => m.scopes.includes(scope))
}

export function metricLabel(value: string): string {
  return RISK_METRIC_DEFINITIONS.find((m) => m.value === value)?.label || value
}

export function formatRiskTime(unixSec: number | undefined): string {
  if (!unixSec) return '—'
  return new Date(unixSec * 1000).toLocaleString([], {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}
