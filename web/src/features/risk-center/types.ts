// ----------------------------------------------------------------------------
// Risk Center types — mirrors backend dto/risk shapes.
// ----------------------------------------------------------------------------

export type RiskScope = 'token' | 'user' | 'ip'

export type RiskAction = 'observe' | 'block'

export type RiskCondition = {
  metric: string
  op: string
  value: number
}

export type RiskRule = {
  id: number
  name: string
  description: string
  enabled: boolean
  scope: RiskScope
  detector: string
  match_mode: 'all' | 'any'
  priority: number
  action: RiskAction
  auto_block: boolean
  auto_recover: boolean
  recover_mode: 'ttl' | 'manual'
  recover_after_seconds: number
  response_status_code: number
  response_message: string
  score_weight: number
  conditions: RiskCondition[]
  groups: string[]
  created_time?: number
}

export type RiskSubject = {
  id: number
  scope: RiskScope
  identifier: string
  username?: string
  status: 'normal' | 'observe' | 'blocked'
  risk_score: number
  hit_count_24h: number
  last_hit_time?: number
  last_decision?: string
  notes?: string
}

export type RiskIncident = {
  id: number
  scope: RiskScope
  identifier: string
  username?: string
  rule_id?: number
  rule_name?: string
  decision: 'allow' | 'observe' | 'block'
  reason?: string
  created_time: number
}

export type RiskOverview = {
  total_subjects?: number
  blocked_subjects?: number
  observed_subjects?: number
  total_incidents_24h?: number
  blocked_incidents_24h?: number
  observed_incidents_24h?: number
  rule_count?: number
  enabled_rule_count?: number
}

export type RiskGroups = {
  items: Array<{
    name: string
    enabled: boolean
    description?: string
  }>
}

export type RiskConfig = Record<string, unknown>

// ----------------------------------------------------------------------------
// Moderation engine types
// ----------------------------------------------------------------------------

export type ModerationRule = {
  id: number
  name: string
  enabled: boolean
  category: string
  threshold: number
  action: RiskAction
  groups: string[]
  description?: string
  priority?: number
  created_time?: number
}

export type ModerationConfig = {
  enabled?: boolean
  provider?: string
  endpoint?: string
  model?: string
  keys_json?: string
  request_timeout_ms?: number
  max_concurrent?: number
  cache_ttl_seconds?: number
  default_action?: RiskAction
  observe_response_message?: string
  block_response_status_code?: number
  block_response_message?: string
  [key: string]: unknown
}

export type ModerationCategory = {
  name: string
  description?: string
  default_threshold?: number
}

export type ModerationIncident = {
  id: number
  rule_id?: number
  rule_name?: string
  category?: string
  score?: number
  decision: 'allow' | 'observe' | 'block'
  user_id?: number
  username?: string
  group?: string
  text_excerpt?: string
  created_time: number
}

export type ModerationQueueStats = {
  in_flight?: number
  queued?: number
  processed_24h?: number
  blocked_24h?: number
}

// ----------------------------------------------------------------------------
// Enforcement engine types
// ----------------------------------------------------------------------------

export type EnforcementConfig = {
  auto_disable_enabled?: boolean
  auto_disable_threshold?: number
  alert_email_enabled?: boolean
  alert_email_recipient?: string
  alert_webhook_url?: string
  cooldown_seconds?: number
  [key: string]: unknown
}

export type EnforcementCounter = {
  scope: RiskScope
  identifier: string
  username?: string
  count: number
  last_action_time?: number
}

export type EnforcementIncident = {
  id: number
  scope: RiskScope
  identifier: string
  action: 'disable' | 'alert' | 'notify'
  reason?: string
  triggered_by?: string
  created_time: number
}

// ----------------------------------------------------------------------------
// Pagination wrappers
// ----------------------------------------------------------------------------

export type Paginated<T> = {
  items: T[]
  page: number
  page_size: number
  total: number
}
