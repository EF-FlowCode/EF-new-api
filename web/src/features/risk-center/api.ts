import { api } from '@/lib/api'
import type {
  EnforcementConfig,
  EnforcementCounter,
  EnforcementIncident,
  ModerationCategory,
  ModerationConfig,
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

type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
}

function unwrap<T>(res: { data: ApiResponse<T> }, fallback: T): T {
  if (!res.data.success) {
    throw new Error(res.data.message || 'Request failed')
  }
  return res.data.data ?? fallback
}

// ============================================================================
// Distribution engine
// ============================================================================

export async function fetchOverview(): Promise<RiskOverview> {
  const res = await api.get<ApiResponse<RiskOverview>>('/api/risk/overview')
  return unwrap(res, {})
}

export async function fetchRiskGroups(): Promise<RiskGroups> {
  const res = await api.get<ApiResponse<RiskGroups>>('/api/risk/groups')
  return unwrap(res, { items: [] })
}

export async function fetchRiskConfig(): Promise<RiskConfig> {
  const res = await api.get<ApiResponse<RiskConfig>>('/api/risk/config')
  return unwrap(res, {})
}

export async function updateRiskConfig(config: RiskConfig): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>('/api/risk/config', config)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update risk config')
  }
  return true
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
function normalizeRule(raw: RiskRule): RiskRule {
  return {
    ...raw,
    conditions: safeParseArray(raw.conditions, [] as RiskRule['conditions']),
    groups: safeParseArray(raw.groups, [] as string[]),
  }
}

export async function fetchRules(): Promise<RiskRule[]> {
  const res = await api.get<ApiResponse<RiskRule[] | null>>('/api/risk/rules')
  const list = unwrap(res, []) ?? []
  return list.map(normalizeRule)
}

export async function createRule(rule: Partial<RiskRule>): Promise<RiskRule | null> {
  const res = await api.post<ApiResponse<RiskRule>>('/api/risk/rules', rule)
  return unwrap(res, null as unknown as RiskRule | null)
}

export async function updateRule(
  id: number,
  rule: Partial<RiskRule>
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    `/api/risk/rules/${id}`,
    rule
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update rule')
  }
  return true
}

export async function deleteRule(id: number): Promise<boolean> {
  const res = await api.delete<ApiResponse<unknown>>(`/api/risk/rules/${id}`)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to delete rule')
  }
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
  q: SubjectQuery
): Promise<Paginated<RiskSubject>> {
  const res = await api.get<ApiResponse<Paginated<RiskSubject>>>(
    '/api/risk/subjects',
    {
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        status: q.status || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return unwrap(res, {
    items: [],
    page: q.page,
    page_size: q.pageSize,
    total: 0,
  })
}

export type IncidentQuery = {
  page: number
  pageSize: number
  scope?: string
  action?: string
  keyword?: string
}

export async function fetchIncidents(
  q: IncidentQuery
): Promise<Paginated<RiskIncident>> {
  const res = await api.get<ApiResponse<Paginated<RiskIncident>>>(
    '/api/risk/incidents',
    {
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        action: q.action || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return unwrap(res, {
    items: [],
    page: q.page,
    page_size: q.pageSize,
    total: 0,
  })
}

export async function detectIp(ip: string): Promise<unknown> {
  const res = await api.post<ApiResponse<unknown>>('/api/risk/detect-ip', { ip })
  return unwrap(res, null)
}

// ============================================================================
// Moderation engine
// ============================================================================

export async function fetchModerationOverview(): Promise<RiskOverview> {
  const res = await api.get<ApiResponse<RiskOverview>>(
    '/api/risk/moderation/overview'
  )
  return unwrap(res, {})
}

export async function fetchModerationConfig(): Promise<ModerationConfig> {
  const res = await api.get<ApiResponse<ModerationConfig>>(
    '/api/risk/moderation/config'
  )
  return unwrap(res, {})
}

export async function updateModerationConfig(
  config: ModerationConfig
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    '/api/risk/moderation/config',
    config
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update moderation config')
  }
  return true
}

export async function fetchModerationCategories(): Promise<
  ModerationCategory[]
> {
  const res = await api.get<ApiResponse<ModerationCategory[] | null>>(
    '/api/risk/moderation/categories'
  )
  return unwrap(res, []) ?? []
}

export async function fetchModerationRules(): Promise<ModerationRule[]> {
  const res = await api.get<ApiResponse<ModerationRule[] | null>>(
    '/api/risk/moderation/rules'
  )
  return unwrap(res, []) ?? []
}

export async function saveModerationRule(
  rule: Partial<ModerationRule>
): Promise<boolean> {
  const path = rule.id
    ? `/api/risk/moderation/rules/${rule.id}`
    : '/api/risk/moderation/rules'
  const fn = rule.id ? api.put : api.post
  const res = await fn<ApiResponse<unknown>>(path, rule)
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to save moderation rule')
  }
  return true
}

export async function deleteModerationRule(id: number): Promise<boolean> {
  const res = await api.delete<ApiResponse<unknown>>(
    `/api/risk/moderation/rules/${id}`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to delete moderation rule')
  }
  return true
}

export async function fetchModerationIncidents(
  q: IncidentQuery
): Promise<Paginated<ModerationIncident>> {
  const res = await api.get<ApiResponse<Paginated<ModerationIncident>>>(
    '/api/risk/moderation/incidents',
    {
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        action: q.action || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return unwrap(res, {
    items: [],
    page: q.page,
    page_size: q.pageSize,
    total: 0,
  })
}

export async function fetchModerationIncidentDetail(
  id: number
): Promise<ModerationIncident & { full_text?: string }> {
  const res = await api.get<ApiResponse<ModerationIncident & { full_text?: string }>>(
    `/api/risk/moderation/incidents/${id}`
  )
  return unwrap(res, {} as never)
}

export async function fetchModerationQueueStats(): Promise<ModerationQueueStats> {
  const res = await api.get<ApiResponse<ModerationQueueStats>>(
    '/api/risk/moderation/queue_stats'
  )
  return unwrap(res, {})
}

export async function debugModeration(payload: {
  text?: string
  images?: string
  group?: string
}): Promise<unknown> {
  const res = await api.post<ApiResponse<unknown>>(
    '/api/risk/moderation/debug',
    payload
  )
  return unwrap(res, null)
}

// ============================================================================
// Enforcement engine
// ============================================================================

export async function fetchEnforcementOverview(): Promise<RiskOverview> {
  const res = await api.get<ApiResponse<RiskOverview>>(
    '/api/risk/enforcement/overview'
  )
  return unwrap(res, {})
}

export async function fetchEnforcementConfig(): Promise<EnforcementConfig> {
  const res = await api.get<ApiResponse<EnforcementConfig>>(
    '/api/risk/enforcement/config'
  )
  return unwrap(res, {})
}

export async function updateEnforcementConfig(
  config: EnforcementConfig
): Promise<boolean> {
  const res = await api.put<ApiResponse<unknown>>(
    '/api/risk/enforcement/config',
    config
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to update enforcement config')
  }
  return true
}

export async function fetchEnforcementCounters(
  q: { page: number; pageSize: number }
): Promise<Paginated<EnforcementCounter>> {
  const res = await api.get<ApiResponse<Paginated<EnforcementCounter>>>(
    '/api/risk/enforcement/counters',
    { params: { p: q.page, page_size: q.pageSize } }
  )
  return unwrap(res, {
    items: [],
    page: q.page,
    page_size: q.pageSize,
    total: 0,
  })
}

export async function fetchEnforcementIncidents(
  q: IncidentQuery
): Promise<Paginated<EnforcementIncident>> {
  const res = await api.get<ApiResponse<Paginated<EnforcementIncident>>>(
    '/api/risk/enforcement/incidents',
    {
      params: {
        p: q.page,
        page_size: q.pageSize,
        scope: q.scope || undefined,
        action: q.action || undefined,
        keyword: q.keyword || undefined,
      },
    }
  )
  return unwrap(res, {
    items: [],
    page: q.page,
    page_size: q.pageSize,
    total: 0,
  })
}

export async function sendEnforcementTestEmail(): Promise<boolean> {
  const res = await api.post<ApiResponse<unknown>>(
    '/api/risk/enforcement/test_email'
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to send test email')
  }
  return true
}
