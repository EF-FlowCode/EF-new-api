export type RiskScope = 'token' | 'user'

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

export type RiskRuleUpsertRequest = Omit<RiskRule, 'id' | 'created_time'>

export type RiskSubject = {
  id: number
  subject_type: RiskScope
  subject_id: number
  username?: string
  status: 'normal' | 'observe' | 'blocked'
  risk_score: number
  rule_hit_count_24h: number
  last_incident_at?: number
  last_decision?: string
  notes?: string
}

export type RiskIncident = {
  id: number
  subject_type: RiskScope
  subject_id: number
  username?: string
  rule_id?: number
  rule_name?: string
  decision: 'allow' | 'observe' | 'flag' | 'block'
  reason?: string
  created_at: number
}

export type RiskOverview = {
  observed_subjects: number
  blocked_subjects: number
  high_risk_subjects: number
  rule_count: number
}

export type ModerationOverview = {
  rule_count: number
  unconfigured_rule_count: number
  flagged_24h: number
}

export type EnforcementOverview = {
  hits_24h: number
  auto_bans_24h: number
  ban_threshold: number
  window_hours: number
}

export type RiskGroups = {
  items: Array<{
    name: string
    enabled: boolean
    description?: string
  }>
}

export type RiskConfig = Record<string, unknown>

export type ModerationCondition = {
  category: string
  op: string
  value: number
  apply_input_type?: boolean
  applied_input_type?: 'text' | 'image'
}

export type ModerationRule = {
  id: number
  name: string
  enabled: boolean
  match_mode: 'all' | 'any'
  conditions: ModerationCondition[]
  action: 'observe' | 'flag' | 'block'
  score_weight: number
  groups: string[]
  description: string
  priority: number
  created_time?: number
}

export type ModerationConfig = {
  enabled: boolean
  mode: 'off' | 'observe_only' | 'enforce'
  base_url: string
  model: string
  api_keys: string[]
  event_queue_size: number
  worker_count: number
  http_timeout_ms: number
  max_retries: number
  redis_queue_enabled: boolean
  sampling_rate_percent: number
  image_max_size_kb: number
  enabled_groups: string[]
  group_modes: Record<string, string>
  debug_result_retain_minutes: number
  flagged_retention_hours: number
  benign_retention_hours: number
  record_unmatched_inputs: boolean
}

export type ModerationConfigResponse = {
  config: ModerationConfig
  key_count: number
}

export type ModerationCategory = {
  name: string
  label: string
  image_scored: boolean
}

export type ModerationIncident = {
  id: number
  rule_id?: number
  primary_rule?: string
  max_category?: string
  max_score?: number
  decision: 'allow' | 'observe' | 'flag' | 'block'
  user_id?: number
  username?: string
  group?: string
  input_summary?: string
  created_at: number
}

export type ModerationQueueStats = {
  queue_depth_memory?: number
  queue_depth_redis?: number
  worker_count?: number
  worker_state?: Array<{
    state: string
  }>
}

export type EnforcementConfig = {
  enabled: boolean
  email_on_hit: boolean
  email_on_auto_ban: boolean
  count_window_hours: number
  ban_threshold: number
  ban_threshold_per_source: Record<string, number>
  enabled_sources: string[]
  email_hit_template: string
  email_ban_template: string
  email_hit_subject: string
  email_ban_subject: string
  hit_email_window_minutes: number
  hit_email_max_per_window: number
  ban_email_window_minutes: number
  ban_email_max_per_window: number
  email_rate_limit_window_minutes?: number
  email_rate_limit_max_per_window?: number
}

export type EnforcementCounter = {
  id: number
  username: string
  enforcement_hit_count_risk: number
  enforcement_hit_count_moderation: number
  enforcement_last_hit_at: number
}

export type EnforcementIncident = {
  id: number
  user_id: number
  source: string
  action: 'hit' | 'auto_ban' | 'manual_unban' | 'counter_reset' | 'test_email'
  reason?: string
  rule_hint?: string
  created_at: number
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
