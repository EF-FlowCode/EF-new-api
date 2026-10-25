import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'
import {
  parseModerationConfig,
  parseEnforcementConfig,
  parseModerationConditions,
  parseModerationKeys,
} from './lib/config-contract'
import type {
  EnforcementConfig,
  EnforcementCounter,
  EnforcementIncident,
  ModerationCategory,
  ModerationConfig,
  ModerationConfigResponse,
  ModerationOverview,
  EnforcementOverview,
  RiskRuleUpsertRequest,
  ModerationIncident,
  ModerationQueueStats,
  ModerationRule,
  Paginated,
  RiskConfig,
  RiskGroups,
  RiskIncident,
  RiskOverview,
  RiskRule,
  RiskSubject,
} from './types'

export async function fetchOverview(): Promise<Partial<RiskOverview>> {
  const res = await api.get<ApiResult<Partial<RiskOverview> | null>>(
    '/api/risk/overview',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? {}
}

export async function fetchRiskGroups(): Promise<RiskGroups> {
  const res = await api.get<ApiResult<RiskGroups | null>>(
    '/api/risk/groups',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? { items: [] }
}

export async function fetchRiskConfig(): Promise<RiskConfig> {
  const res = await api.get<ApiResult<RiskConfig | null>>(
    '/api/risk/config',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? {}
}

export async function updateRiskConfig(config: RiskConfig): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    '/api/risk/config',
    config,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update risk config')
  return true
}

function normalizePage<T>(
  page: Paginated<T> | null | undefined
): Paginated<T> | null {
  return page ? { ...page, items: page.items ?? [] } : null
}

function safeParseArray<T>(value: unknown, fallback: T[]): T[] {
  if (Array.isArray(value)) return value as T[]
  if (typeof value === 'string') {
    if (!value.trim()) return fallback
    try {
      const parsed = JSON.parse(value)
      return Array.isArray(parsed) ? (parsed as T[]) : fallback
    } catch {
      return fallback
    }
  }
  return fallback
}

/**
 * Backend serializes RiskRule.{conditions,groups} as JSON-encoded strings;
 * normalize once on read so the rest of the UI can rely on real arrays.
 */
function normalizeRule(raw: RiskRule & { created_at?: number }): RiskRule {
  return {
    ...raw,
    created_time: raw.created_at ?? raw.created_time,
    conditions: safeParseArray(raw.conditions, [] as RiskRule['conditions']),
    groups: safeParseArray(raw.groups, [] as string[]),
  }
}

export async function fetchRules(): Promise<RiskRule[]> {
  const res = await api.get<ApiResult<RiskRule[] | null>>(
    '/api/risk/rules',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  const list = unwrapApiResult(res.data, 'Request failed') ?? []
  return list.map(normalizeRule)
}

export async function createRule(
  rule: RiskRuleUpsertRequest
): Promise<RiskRule | null> {
  const res = await api.post<ApiResult<RiskRule | null>>(
    '/api/risk/rules',
    rule,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  const created = unwrapApiResult(res.data, 'Request failed')
  return created ? normalizeRule(created) : null
}

export async function updateRule(
  id: number,
  rule: RiskRuleUpsertRequest
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    `/api/risk/rules/${id}`,
    rule,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update rule')
  return true
}

export async function deleteRule(id: number): Promise<boolean> {
  const res = await api.delete<ApiResult<unknown>>(
    `/api/risk/rules/${id}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to delete rule')
  return true
}

export type SubjectQuery = {
  page: number
  pageSize: number
  scope?: string
  status?: string
  keyword?: string
}

export async function fetchSubjects(
  q: SubjectQuery,
  signal?: AbortSignal
): Promise<Paginated<RiskSubject>> {
  const res = await api.get<ApiResult<Paginated<RiskSubject> | null>>(
    '/api/risk/subjects',
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      signal,
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        status: q.status || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return (
    normalizePage(unwrapApiResult(res.data, 'Request failed')) ?? {
      items: [],
      page: q.page,
      page_size: q.pageSize,
      total: 0,
    }
  )
}

export type IncidentQuery = {
  page: number
  pageSize: number
  scope?: string
  action?: string
  keyword?: string
}

export async function fetchIncidents(
  q: IncidentQuery,
  signal?: AbortSignal
): Promise<Paginated<RiskIncident>> {
  const res = await api.get<ApiResult<Paginated<RiskIncident> | null>>(
    '/api/risk/incidents',
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      signal,
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        action: q.action || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return (
    normalizePage(unwrapApiResult(res.data, 'Request failed')) ?? {
      items: [],
      page: q.page,
      page_size: q.pageSize,
      total: 0,
    }
  )
}

export async function detectIp(): Promise<unknown> {
  const res = await api.get<ApiResult<unknown>>(
    '/api/risk/detect-ip',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? null
}

export async function fetchModerationOverview(): Promise<
  Partial<ModerationOverview>
> {
  const res = await api.get<ApiResult<Partial<ModerationOverview> | null>>(
    '/api/risk/moderation/overview',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? {}
}

export async function fetchModerationConfig(): Promise<ModerationConfig> {
  const res = await api.get<ApiResult<ModerationConfigResponse | null>>(
    '/api/risk/moderation/config',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return parseModerationConfig(
    unwrapApiResult(res.data, 'Request failed')?.config
  )
}

export async function updateModerationConfig(
  config: ModerationConfig
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    '/api/risk/moderation/config',
    parseModerationConfig(config),
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update moderation config')
  return true
}

export async function fetchModerationCategories(): Promise<
  ModerationCategory[]
> {
  const res = await api.get<ApiResult<ModerationCategory[] | null>>(
    '/api/risk/moderation/categories',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? []
}

export async function fetchModerationRules(): Promise<ModerationRule[]> {
  const res = await api.get<
    ApiResult<Array<
      Omit<ModerationRule, 'conditions' | 'groups'> & {
        conditions: string
        groups: string
        created_at: number
      }
    > | null>
  >('/api/risk/moderation/rules', CALLER_HANDLED_REQUEST_CONFIG)
  return (unwrapApiResult(res.data, 'Request failed') ?? []).map((rule) => ({
    ...rule,
    created_time: rule.created_at,
    conditions: parseModerationConditions(rule.conditions),
    groups:
      !rule.groups?.trim() || rule.groups.trim() === 'null'
        ? []
        : parseModerationKeys(rule.groups),
  }))
}

export async function saveModerationRule(
  rule: ModerationRule
): Promise<boolean> {
  const path = rule.id
    ? `/api/risk/moderation/rules/${rule.id}`
    : '/api/risk/moderation/rules'
  const payload = {
    name: rule.name,
    description: rule.description,
    enabled: rule.enabled,
    match_mode: rule.match_mode,
    action: rule.action,
    priority: rule.priority,
    score_weight: rule.score_weight,
    conditions: rule.conditions,
    groups: rule.groups,
  }
  const res = rule.id
    ? await api.put<ApiResult<unknown>>(
        path,
        payload,
        CALLER_HANDLED_REQUEST_CONFIG
      )
    : await api.post<ApiResult<unknown>>(
        path,
        payload,
        CALLER_HANDLED_REQUEST_CONFIG
      )
  unwrapApiResult(res.data, 'Failed to save moderation rule')
  return true
}

export async function deleteModerationRule(id: number): Promise<boolean> {
  const res = await api.delete<ApiResult<unknown>>(
    `/api/risk/moderation/rules/${id}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to delete moderation rule')
  return true
}

export async function fetchModerationIncidents(
  q: IncidentQuery,
  signal?: AbortSignal
): Promise<Paginated<ModerationIncident>> {
  const res = await api.get<ApiResult<Paginated<ModerationIncident> | null>>(
    '/api/risk/moderation/incidents',
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      signal,
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        action: q.action || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return (
    normalizePage(unwrapApiResult(res.data, 'Request failed')) ?? {
      items: [],
      page: q.page,
      page_size: q.pageSize,
      total: 0,
    }
  )
}

export async function fetchModerationIncidentDetail(
  id: number
): Promise<ModerationIncident & { full_text?: string }> {
  const res = await api.get<
    ApiResult<(ModerationIncident & { full_text?: string }) | null>
  >(`/api/risk/moderation/incidents/${id}`, CALLER_HANDLED_REQUEST_CONFIG)
  return unwrapApiResult(res.data, 'Request failed') ?? ({} as never)
}

export async function fetchModerationQueueStats(): Promise<ModerationQueueStats> {
  const res = await api.get<ApiResult<ModerationQueueStats | null>>(
    '/api/risk/moderation/queue_stats',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? {}
}

export async function debugModeration(payload: {
  text?: string
  images?: string
  group?: string
}): Promise<unknown> {
  const res = await api.post<ApiResult<unknown>>(
    '/api/risk/moderation/debug',
    payload,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? null
}

export async function fetchEnforcementOverview(): Promise<
  Partial<EnforcementOverview>
> {
  const res = await api.get<ApiResult<Partial<EnforcementOverview> | null>>(
    '/api/risk/enforcement/overview',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Request failed') ?? {}
}

export async function fetchEnforcementConfig(): Promise<EnforcementConfig> {
  const res = await api.get<ApiResult<EnforcementConfig | null>>(
    '/api/risk/enforcement/config',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return parseEnforcementConfig(unwrapApiResult(res.data, 'Request failed'))
}

export async function updateEnforcementConfig(
  config: EnforcementConfig
): Promise<boolean> {
  const res = await api.put<ApiResult<unknown>>(
    '/api/risk/enforcement/config',
    parseEnforcementConfig(config),
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to update enforcement config')
  return true
}

export async function fetchEnforcementCounters(
  q: {
    page: number
    pageSize: number
  },
  signal?: AbortSignal
): Promise<Paginated<EnforcementCounter>> {
  const res = await api.get<ApiResult<Paginated<EnforcementCounter> | null>>(
    '/api/risk/enforcement/counters',
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      signal,
      params: { p: q.page, page_size: q.pageSize },
    }
  )
  return (
    normalizePage(unwrapApiResult(res.data, 'Request failed')) ?? {
      items: [],
      page: q.page,
      page_size: q.pageSize,
      total: 0,
    }
  )
}

export async function fetchEnforcementIncidents(
  q: IncidentQuery,
  signal?: AbortSignal
): Promise<Paginated<EnforcementIncident>> {
  const res = await api.get<ApiResult<Paginated<EnforcementIncident> | null>>(
    '/api/risk/enforcement/incidents',
    {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      signal,
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        action: q.action || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return (
    normalizePage(unwrapApiResult(res.data, 'Request failed')) ?? {
      items: [],
      page: q.page,
      page_size: q.pageSize,
      total: 0,
    }
  )
}

export async function sendEnforcementTestEmail(): Promise<boolean> {
  const res = await api.post<ApiResult<unknown>>(
    '/api/risk/enforcement/test_email',
    undefined,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to send test email')
  return true
}
