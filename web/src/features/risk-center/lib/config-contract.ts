import type {
  EnforcementConfig,
  ModerationConfig,
  ModerationCondition,
} from '../types'

export function parseModerationConfig(value: unknown): ModerationConfig {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new Error('Invalid moderation configuration')
  }
  const config = value as Record<string, unknown>
  const numbers = [
    'event_queue_size',
    'worker_count',
    'http_timeout_ms',
    'max_retries',
    'sampling_rate_percent',
    'image_max_size_kb',
    'debug_result_retain_minutes',
    'flagged_retention_hours',
    'benign_retention_hours',
  ]
  const booleans = ['enabled', 'redis_queue_enabled', 'record_unmatched_inputs']
  if (
    !['off', 'observe_only', 'enforce'].includes(String(config.mode)) ||
    typeof config.base_url !== 'string' ||
    typeof config.model !== 'string' ||
    numbers.some(
      (key) => typeof config[key] !== 'number' || !Number.isFinite(config[key])
    ) ||
    booleans.some((key) => typeof config[key] !== 'boolean') ||
    !Array.isArray(config.api_keys) ||
    !config.api_keys.every((key) => typeof key === 'string') ||
    !Array.isArray(config.enabled_groups) ||
    !config.enabled_groups.every((group) => typeof group === 'string') ||
    !config.group_modes ||
    typeof config.group_modes !== 'object' ||
    Array.isArray(config.group_modes) ||
    !Object.values(config.group_modes).every((mode) => typeof mode === 'string')
  ) {
    throw new Error('Invalid moderation configuration')
  }
  return value as ModerationConfig
}

export function parseModerationKeys(text: string): string[] {
  const value: unknown = JSON.parse(text)
  if (!Array.isArray(value) || !value.every((key) => typeof key === 'string')) {
    throw new Error('Invalid JSON')
  }
  return value
}

export function parseModerationConditions(text: string): ModerationCondition[] {
  const value: unknown = JSON.parse(text)
  if (
    !Array.isArray(value) ||
    !value.every((item: unknown) => {
      if (!item || typeof item !== 'object' || Array.isArray(item)) return false
      const condition = item as Record<string, unknown>
      return (
        typeof condition.category === 'string' &&
        typeof condition.op === 'string' &&
        typeof condition.value === 'number' &&
        Number.isFinite(condition.value) &&
        condition.value >= 0 &&
        condition.value <= 1 &&
        (condition.apply_input_type === undefined ||
          typeof condition.apply_input_type === 'boolean') &&
        (condition.applied_input_type === undefined ||
          condition.applied_input_type === 'text' ||
          condition.applied_input_type === 'image')
      )
    })
  )
    throw new Error('Invalid moderation conditions')
  return value as ModerationCondition[]
}

export function parseEnforcementConfig(value: unknown): EnforcementConfig {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('Invalid enforcement configuration')
  const config = value as Record<string, unknown>
  if (
    ['enabled', 'email_on_hit', 'email_on_auto_ban'].some(
      (key) => typeof config[key] !== 'boolean'
    ) ||
    [
      'count_window_hours',
      'ban_threshold',
      'hit_email_window_minutes',
      'hit_email_max_per_window',
      'ban_email_window_minutes',
      'ban_email_max_per_window',
    ].some(
      (key) => typeof config[key] !== 'number' || !Number.isFinite(config[key])
    ) ||
    [
      'email_hit_template',
      'email_ban_template',
      'email_hit_subject',
      'email_ban_subject',
    ].some((key) => typeof config[key] !== 'string') ||
    !Array.isArray(config.enabled_sources) ||
    !config.enabled_sources.every((source) => typeof source === 'string') ||
    !config.ban_threshold_per_source ||
    typeof config.ban_threshold_per_source !== 'object' ||
    Array.isArray(config.ban_threshold_per_source) ||
    !Object.values(config.ban_threshold_per_source).every(
      (threshold) => typeof threshold === 'number' && Number.isFinite(threshold)
    )
  )
    throw new Error('Invalid enforcement configuration')
  return value as EnforcementConfig
}
