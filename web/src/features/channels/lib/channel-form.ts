import { z } from 'zod'
import {
  BASE_URL_REQUIRED_TYPES,
  CHANNEL_STATUS,
  CLAUDE_FIELD_PASSTHROUGH_TYPES,
  ERROR_MESSAGES,
  OPENAI_FIELD_PASSTHROUGH_TYPES,
  isModelFetchableChannel,
} from '../constants'
import type { Channel } from '../types'
import {
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
} from './advanced-custom'
import {
  channelRateLimitSchema,
  errorFilterRuleSchema,
  riskControlHeaderRuleSchema,
  DEFAULT_CHANNEL_RATE_LIMIT,
  normalizeChannelRateLimit,
  normalizeErrorFilterRules,
  normalizeRiskControlHeaders,
  type ChannelRateLimit,
  type ErrorFilterRule,
  type RiskControlHeaderRule,
} from './channel-extra-rules'
import { supportsEndpointOverride } from './endpoint-support'

// ============================================================================
// Form Validation Schema
// ============================================================================

const SUPPORTED_PROXY_PROTOCOLS = new Set([
  'http:',
  'https:',
  'socks5:',
  'socks5h:',
])

export const HTTP_PROTOCOL_AUTO = 'auto'
export const HTTP_PROTOCOL_HTTP1 = 'http1'
export const MAX_HTTP2_CONNECTION_SHARDS = 8

function isOptionalProxyURL(value: string | undefined): boolean {
  const trimmed = value?.trim() || ''
  if (!trimmed) return true

  const separator = trimmed.indexOf('://')
  if (separator <= 0) return false

  const authorityAndSuffix = trimmed.slice(separator + 3)
  const suffixIndex = authorityAndSuffix.search(/[/?#]/)
  if (suffixIndex >= 0 && authorityAndSuffix.slice(suffixIndex) !== '/') {
    return false
  }

  try {
    const parsed = new URL(trimmed)
    return (
      SUPPORTED_PROXY_PROTOCOLS.has(parsed.protocol) &&
      Boolean(parsed.hostname) &&
      parsed.port !== '0'
    )
  } catch {
    return false
  }
}

export function normalizeHttpProtocol(
  value: string | undefined | null
): 'auto' | 'http1' {
  return String(value || '')
    .trim()
    .toLowerCase() === HTTP_PROTOCOL_HTTP1
    ? HTTP_PROTOCOL_HTTP1
    : HTTP_PROTOCOL_AUTO
}

export function normalizeHttp2ConnectionShards(
  value: number | undefined | null
): number {
  if (!value || Number.isNaN(value)) return 1
  return Math.min(Math.max(value, 1), MAX_HTTP2_CONNECTION_SHARDS)
}

export const channelFormSchema = z
  .object({
    name: z.string().min(1, 'Channel name is required'),
    type: z.number().min(0, 'Channel type is required'),
    base_url: z.string().optional(),
    key: z.string(),
    openai_organization: z.string().optional(),
    models: z.string().min(1, 'At least one model is required'),
    group: z.array(z.string()).min(1, 'At least one group is required'),
    model_mapping: z.string().optional(),
    priority: z.number().optional(),
    weight: z.number().optional(),
    test_model: z.string().optional(),
    auto_ban: z.number().optional(),
    status: z.number(),
    status_code_mapping: z.string().optional(),
    tag: z.string().optional(),
    remark: z
      .string()
      .max(255, 'Remark must be less than 255 characters')
      .optional(),
    setting: z.string().optional(),
    param_override: z.string().optional(),
    header_override: z.string().optional(),
    settings: z.string().optional(),
    other: z.string().optional(),
    // Multi-key options (not sent to backend directly)
    multi_key_mode: z.enum(['single', 'batch', 'multi_to_single']).optional(),
    multi_key_type: z.enum(['random', 'polling']).optional(),
    batch_add_set_key_prefix_2_name: z.boolean().optional(),
    key_mode: z.enum(['append', 'replace']).optional(), // For editing multi-key channels
    // Channel extra settings (stored in setting JSON, not sent directly)
    force_format: z.boolean().optional(),
    thinking_to_content: z.boolean().optional(),
    proxy: z
      .string()
      .optional()
      .refine(isOptionalProxyURL, ERROR_MESSAGES.INVALID_PROXY),
    http_protocol: z.enum(['auto', 'http1']).optional(),
    http2_connection_shards: z.number().int().optional(),
    pass_through_body_enabled: z.boolean().optional(),
    system_prompt: z.string().optional(),
    system_prompt_override: z.boolean().optional(),
    // Type-specific settings (stored in settings JSON)
    supported_endpoint_types: z.array(z.string()).optional(),
    is_enterprise_account: z.boolean().optional(), // OpenRouter specific
    vertex_key_type: z.enum(['json', 'api_key']).optional(), // Vertex AI specific
    vertex_project_id: z.string().optional(), // Vertex AI specific (api_key mode only)
    aws_key_type: z.enum(['ak_sk', 'api_key']).optional(), // AWS specific
    aws_api_type: z.enum(['invoke_model', 'mantle']).optional(), // AWS interface
    azure_responses_version: z.string().optional(), // Azure specific
    // Field passthrough controls (stored in settings JSON)
    allow_service_tier: z.boolean().optional(), // OpenAI/Anthropic
    disable_store: z.boolean().optional(), // OpenAI only
    allow_safety_identifier: z.boolean().optional(), // OpenAI only
    allow_include_obfuscation: z.boolean().optional(), // OpenAI: include usage obfuscation
    allow_inference_geo: z.boolean().optional(), // OpenAI/Anthropic: inference geography
    allow_speed: z.boolean().optional(), // Anthropic: speed mode control
    claude_beta_query: z.boolean().optional(), // Anthropic: beta query passthrough
    // Advanced route configuration is stored in channel `settings`. In eflow
    // it is currently available through the legacy Custom channel (type 8).
    advanced_custom: z.string().optional(),
    // Upstream model update settings (stored in settings JSON)
    upstream_model_update_check_enabled: z.boolean().optional(),
    upstream_model_update_auto_sync_enabled: z.boolean().optional(),
    upstream_model_update_ignored_models: z.string().optional(),
    ratio_sync_access_token: z.string().optional(),
    ratio_sync_user_id: z.number().int().min(0).optional(),
    // Custom: rate limit / risk control headers / error filter rules
    // Stored in `setting` JSON (singular) alongside force_format etc.
    rate_limit: channelRateLimitSchema.optional(),
    risk_control_headers: z.array(riskControlHeaderRuleSchema).optional(),
    error_filter_rules: z.array(errorFilterRuleSchema).optional(),
  })
  .superRefine((data, ctx) => {
    const shards = data.http2_connection_shards ?? 1
    if (shards < 1 || shards > MAX_HTTP2_CONNECTION_SHARDS) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['http2_connection_shards'],
        message: ERROR_MESSAGES.INVALID_HTTP2_CONNECTION_SHARDS,
      })
    }
    if (data.http_protocol === HTTP_PROTOCOL_HTTP1 && shards > 1) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['http2_connection_shards'],
        message: ERROR_MESSAGES.INVALID_HTTP1_WITH_SHARDS,
      })
    }
    if (BASE_URL_REQUIRED_TYPES.has(data.type) && !data.base_url?.trim()) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        path: ['base_url'],
        message: ERROR_MESSAGES.REQUIRED_BASE_URL,
      })
    }
  })

export type ChannelFormValues = z.infer<typeof channelFormSchema>

// ============================================================================
// Default Form Values
// ============================================================================

export const CHANNEL_FORM_DEFAULT_VALUES: ChannelFormValues = {
  name: '',
  type: 1,
  base_url: '',
  key: '',
  openai_organization: '',
  models: '',
  group: ['default'],
  model_mapping: '',
  priority: 0,
  weight: 0,
  test_model: '',
  auto_ban: 1,
  status: CHANNEL_STATUS.ENABLED,
  status_code_mapping: '',
  tag: '',
  remark: '',
  setting: '',
  param_override: '',
  header_override: '',
  settings: '{}',
  other: '',
  multi_key_mode: 'single',
  multi_key_type: 'random',
  batch_add_set_key_prefix_2_name: false,
  key_mode: 'append',
  // Channel extra settings
  force_format: false,
  thinking_to_content: false,
  proxy: '',
  http_protocol: HTTP_PROTOCOL_AUTO,
  http2_connection_shards: 1,
  pass_through_body_enabled: false,
  system_prompt: '',
  system_prompt_override: false,
  // Type-specific settings
  supported_endpoint_types: [],
  is_enterprise_account: false,
  vertex_key_type: 'json',
  vertex_project_id: '',
  aws_key_type: 'ak_sk',
  aws_api_type: 'invoke_model',
  azure_responses_version: '',
  // Field passthrough controls
  allow_service_tier: false,
  disable_store: false,
  allow_safety_identifier: false,
  allow_include_obfuscation: false,
  allow_inference_geo: false,
  allow_speed: false,
  claude_beta_query: false,
  advanced_custom: '',
  upstream_model_update_check_enabled: false,
  upstream_model_update_auto_sync_enabled: false,
  upstream_model_update_ignored_models: '',
  ratio_sync_access_token: '',
  ratio_sync_user_id: 0,
  rate_limit: { ...DEFAULT_CHANNEL_RATE_LIMIT },
  risk_control_headers: [],
  error_filter_rules: [],
}

// ============================================================================
// Transform Functions
// ============================================================================

/**
 * 把可空 JSON 字符串字段做防御性归一：
 * 历史 channel 数据里 `model_mapping` / `status_code_mapping` 等 JSON 字段可能被存为
 * 字面字符串 "null" / "undefined" / 纯空白；这些值会让 `validateModelMappingJson`
 * 走 JSON.parse 后判定为非 object 并拒绝保存，造成"打开编辑直接保存就报错、
 * 加一行再删一行才能存"的迷之 UX bug。统一在加载阶段当作空串处理。
 */
function normalizeOptionalJsonString(value: string | null | undefined): string {
  if (typeof value !== 'string') return ''
  const trimmed = value.trim()
  if (!trimmed) return ''
  const lower = trimmed.toLowerCase()
  if (lower === 'null' || lower === 'undefined') return ''
  return value
}

/**
 * Transform Channel from API to Form default values
 */
export function transformChannelToFormDefaults(
  channel: Channel
): ChannelFormValues {
  // Parse channel extra settings from setting field
  let extraSettings = {
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    http_protocol: HTTP_PROTOCOL_AUTO as 'auto' | 'http1',
    http2_connection_shards: 1,
    pass_through_body_enabled: false,
    system_prompt: '',
    system_prompt_override: false,
  }
  let rateLimit: ChannelRateLimit = { ...DEFAULT_CHANNEL_RATE_LIMIT }
  let riskControlHeaders: RiskControlHeaderRule[] = []
  let errorFilterRules: ErrorFilterRule[] = []

  if (channel.setting) {
    try {
      const parsed = JSON.parse(channel.setting)
      const protocol = normalizeHttpProtocol(parsed.http_protocol)
      extraSettings = {
        force_format: parsed.force_format || false,
        thinking_to_content: parsed.thinking_to_content || false,
        proxy: parsed.proxy || '',
        http_protocol: protocol,
        http2_connection_shards:
          protocol === HTTP_PROTOCOL_HTTP1
            ? 1
            : normalizeHttp2ConnectionShards(parsed.http2_connection_shards),
        pass_through_body_enabled: parsed.pass_through_body_enabled || false,
        system_prompt: parsed.system_prompt || '',
        system_prompt_override: parsed.system_prompt_override || false,
      }
      rateLimit = normalizeChannelRateLimit(parsed.rate_limit)
      riskControlHeaders = normalizeRiskControlHeaders(
        parsed.risk_control_headers
      )
      errorFilterRules = normalizeErrorFilterRules(parsed.error_filter_rules)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel setting:', error)
    }
  }

  // Parse type-specific settings from settings field
  let vertexKeyType: 'json' | 'api_key' = 'json'
  let vertexProjectId = ''
  let supportedEndpointTypes: string[] = []
  let azureResponsesVersion = ''
  let isEnterpriseAccount = false
  let awsKeyType: 'ak_sk' | 'api_key' = 'ak_sk'
  let awsApiType: 'invoke_model' | 'mantle' = 'invoke_model'
  let allowServiceTier = false
  let disableStore = false
  let allowSafetyIdentifier = false
  let allowIncludeObfuscation = false
  let allowInferenceGeo = false
  let allowSpeed = false
  let claudeBetaQuery = false
  let advancedCustom = ''
  let upstreamModelUpdateCheckEnabled = false
  let upstreamModelUpdateAutoSyncEnabled = false
  let upstreamModelUpdateIgnoredModels = ''
  let ratioSyncAccessToken = ''
  let ratioSyncUserID = 0

  if (channel.settings) {
    try {
      const parsed = JSON.parse(channel.settings)
      vertexKeyType = parsed.vertex_key_type || 'json'
      vertexProjectId = parsed.vertex_project_id || ''
      supportedEndpointTypes = Array.isArray(parsed.supported_endpoint_types)
        ? parsed.supported_endpoint_types.filter(
            (value: unknown): value is string => typeof value === 'string'
          )
        : []
      azureResponsesVersion = parsed.azure_responses_version || ''
      isEnterpriseAccount = parsed.openrouter_enterprise === true
      awsKeyType = parsed.aws_key_type || 'ak_sk'
      awsApiType = parsed.aws_api_type === 'mantle' ? 'mantle' : 'invoke_model'
      allowServiceTier = parsed.allow_service_tier === true
      disableStore = parsed.disable_store === true
      allowSafetyIdentifier = parsed.allow_safety_identifier === true
      allowIncludeObfuscation = parsed.allow_include_obfuscation === true
      allowInferenceGeo = parsed.allow_inference_geo === true
      allowSpeed = parsed.allow_speed === true
      claudeBetaQuery = parsed.claude_beta_query === true
      if (parsed.advanced_custom) {
        const config = parseAdvancedCustomConfig(
          JSON.stringify(parsed.advanced_custom)
        )
        if (config) advancedCustom = stringifyAdvancedCustomConfig(config)
      }
      upstreamModelUpdateCheckEnabled =
        parsed.upstream_model_update_check_enabled === true
      upstreamModelUpdateAutoSyncEnabled =
        parsed.upstream_model_update_auto_sync_enabled === true
      upstreamModelUpdateIgnoredModels = Array.isArray(
        parsed.upstream_model_update_ignored_models
      )
        ? parsed.upstream_model_update_ignored_models.join(',')
        : ''
      ratioSyncAccessToken = parsed.ratio_sync_access_token || ''
      ratioSyncUserID = Number(parsed.ratio_sync_user_id) || 0
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse channel settings:', error)
    }
  }

  return {
    name: channel.name || '',
    type: channel.type,
    base_url: channel.base_url || '',
    key: '', // Never populate key from backend for security
    openai_organization: channel.openai_organization || '',
    models: channel.models || '',
    group: parseGroups(channel.group || 'default'),
    model_mapping: normalizeOptionalJsonString(channel.model_mapping),
    priority: channel.priority || 0,
    weight: channel.weight || 0,
    test_model: channel.test_model || '',
    auto_ban: channel.auto_ban ?? 1,
    status: channel.status,
    status_code_mapping: normalizeOptionalJsonString(
      channel.status_code_mapping
    ),
    tag: channel.tag || '',
    remark: channel.remark || '',
    setting: channel.setting || '',
    param_override: channel.param_override || '',
    header_override: channel.header_override || '',
    settings: channel.settings || '{}',
    other: channel.other || '',
    multi_key_mode: 'single',
    multi_key_type: channel.channel_info.multi_key_mode || 'random',
    batch_add_set_key_prefix_2_name: false,
    key_mode: 'append', // Default to append mode for editing multi-key channels
    // Channel extra settings
    ...extraSettings,
    // Type-specific settings
    supported_endpoint_types: supportedEndpointTypes,
    is_enterprise_account: isEnterpriseAccount,
    vertex_key_type: vertexKeyType,
    vertex_project_id: vertexProjectId,
    azure_responses_version: azureResponsesVersion,
    aws_key_type: awsKeyType,
    aws_api_type: awsApiType,
    allow_service_tier: allowServiceTier,
    disable_store: disableStore,
    allow_include_obfuscation: allowIncludeObfuscation,
    allow_inference_geo: allowInferenceGeo,
    allow_speed: allowSpeed,
    claude_beta_query: claudeBetaQuery,
    advanced_custom: advancedCustom,
    allow_safety_identifier: allowSafetyIdentifier,
    upstream_model_update_check_enabled: upstreamModelUpdateCheckEnabled,
    upstream_model_update_auto_sync_enabled: upstreamModelUpdateAutoSyncEnabled,
    upstream_model_update_ignored_models: upstreamModelUpdateIgnoredModels,
    ratio_sync_access_token: ratioSyncAccessToken,
    ratio_sync_user_id: ratioSyncUserID,
    rate_limit: rateLimit,
    risk_control_headers: riskControlHeaders,
    error_filter_rules: errorFilterRules,
  }
}

/**
 * Build the setting JSON string from form extra settings
 */
export function buildSettingJSON(formData: ChannelFormValues): string {
  const rateLimit = normalizeChannelRateLimit(formData.rate_limit)
  const riskControlHeaders = normalizeRiskControlHeaders(
    formData.risk_control_headers
  )
  const errorFilterRules = normalizeErrorFilterRules(
    formData.error_filter_rules
  )

  const settingObj: Record<string, unknown> = {
    force_format: formData.force_format || false,
    thinking_to_content: formData.thinking_to_content || false,
    proxy: formData.proxy?.trim() || '',
    pass_through_body_enabled: formData.pass_through_body_enabled || false,
    system_prompt: formData.system_prompt || '',
    system_prompt_override: formData.system_prompt_override || false,
  }

  const protocol = normalizeHttpProtocol(formData.http_protocol)
  const shards =
    protocol === HTTP_PROTOCOL_HTTP1
      ? 1
      : normalizeHttp2ConnectionShards(formData.http2_connection_shards)
  if (protocol === HTTP_PROTOCOL_HTTP1) {
    settingObj.http_protocol = HTTP_PROTOCOL_HTTP1
  } else if (shards > 1) {
    settingObj.http2_connection_shards = shards
  }

  // Only include rate_limit if it's enabled or has non-default values to keep
  // the JSON tidy when the feature is unused.
  if (rateLimit.enabled) {
    settingObj.rate_limit = rateLimit
  }
  if (riskControlHeaders.length > 0) {
    settingObj.risk_control_headers = riskControlHeaders
  }
  if (errorFilterRules.length > 0) {
    settingObj.error_filter_rules = errorFilterRules
  }
  return JSON.stringify(settingObj)
}

/**
 * Build the settings JSON string (for type-specific config like vertex_key_type)
 */
function buildSettingsJSON(formData: ChannelFormValues): string {
  let settingsObj: Record<string, unknown> = {}

  // Try to parse existing settings first
  if (formData.settings && formData.settings !== '{}') {
    try {
      settingsObj = JSON.parse(formData.settings)
    } catch (error) {
      // eslint-disable-next-line no-console
      console.error('Failed to parse existing settings:', error)
    }
  }

  // Advanced Custom and legacy Custom channels ignore the override server-side.
  if (
    supportsEndpointOverride(formData.type) &&
    formData.supported_endpoint_types?.length
  ) {
    settingsObj.supported_endpoint_types = Array.from(
      new Set(formData.supported_endpoint_types)
    )
  } else {
    delete settingsObj.supported_endpoint_types
  }

  // Add vertex_key_type for Vertex AI channels (type 41)
  if (formData.type === 41) {
    settingsObj.vertex_key_type = formData.vertex_key_type || 'json'
    const vertexProjectId = formData.vertex_project_id?.trim()
    if (settingsObj.vertex_key_type === 'api_key' && vertexProjectId) {
      settingsObj.vertex_project_id = vertexProjectId
    } else {
      delete settingsObj.vertex_project_id
    }
  } else {
    delete settingsObj.vertex_key_type
    delete settingsObj.vertex_project_id
  }

  // Add azure_responses_version for Azure channels (type 3)
  if (formData.type === 3 && formData.azure_responses_version) {
    settingsObj.azure_responses_version = formData.azure_responses_version
  } else if ('azure_responses_version' in settingsObj) {
    delete settingsObj.azure_responses_version
  }

  // Add enterprise account setting for OpenRouter (type 20)
  if (formData.type === 20) {
    settingsObj.openrouter_enterprise = formData.is_enterprise_account === true
  } else if ('openrouter_enterprise' in settingsObj) {
    delete settingsObj.openrouter_enterprise
  }

  // Add aws_key_type for AWS channels (type 33). The interface selection is
  // opt-in; existing channels retain InvokeModel.
  if (formData.type === 33) {
    settingsObj.aws_key_type = formData.aws_key_type || 'ak_sk'
    settingsObj.aws_api_type = formData.aws_api_type || 'invoke_model'
  } else {
    delete settingsObj.aws_key_type
    delete settingsObj.aws_api_type
  }

  // Field passthrough controls are keyed by eflow's actual channel mapping.
  // In particular, eflow's Ali media channels (1001–1004) must not inherit the
  // upstream Advanced Custom/Sub2API/New API controls.
  if (
    OPENAI_FIELD_PASSTHROUGH_TYPES.has(formData.type) ||
    CLAUDE_FIELD_PASSTHROUGH_TYPES.has(formData.type)
  ) {
    settingsObj.allow_service_tier = formData.allow_service_tier === true
  } else if ('allow_service_tier' in settingsObj) {
    delete settingsObj.allow_service_tier
  }

  if (OPENAI_FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.disable_store = formData.disable_store === true
    settingsObj.allow_safety_identifier =
      formData.allow_safety_identifier === true
    settingsObj.allow_include_obfuscation =
      formData.allow_include_obfuscation === true
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
  } else {
    if ('disable_store' in settingsObj) delete settingsObj.disable_store
    if ('allow_safety_identifier' in settingsObj)
      delete settingsObj.allow_safety_identifier
    if ('allow_include_obfuscation' in settingsObj)
      delete settingsObj.allow_include_obfuscation
    if (formData.type !== 14 && 'allow_inference_geo' in settingsObj)
      delete settingsObj.allow_inference_geo
  }

  // Anthropic (type 14): claude_beta_query, allow_inference_geo, allow_speed.
  if (CLAUDE_FIELD_PASSTHROUGH_TYPES.has(formData.type)) {
    settingsObj.allow_inference_geo = formData.allow_inference_geo === true
    settingsObj.allow_speed = formData.allow_speed === true
    settingsObj.claude_beta_query = formData.claude_beta_query === true
  } else {
    if ('allow_speed' in settingsObj) delete settingsObj.allow_speed
    if ('claude_beta_query' in settingsObj) delete settingsObj.claude_beta_query
  }

  if (formData.type === 8 && formData.advanced_custom?.trim()) {
    const config = parseAdvancedCustomConfig(formData.advanced_custom)
    if (config) {
      settingsObj.advanced_custom = config
    }
  } else if ('advanced_custom' in settingsObj) {
    delete settingsObj.advanced_custom
  }

  // Upstream model update settings (for model-fetchable channel types)
  if (isModelFetchableChannel(formData.type, formData.advanced_custom)) {
    settingsObj.upstream_model_update_check_enabled =
      formData.upstream_model_update_check_enabled === true
    settingsObj.upstream_model_update_auto_sync_enabled =
      settingsObj.upstream_model_update_check_enabled === true &&
      formData.upstream_model_update_auto_sync_enabled === true
    settingsObj.upstream_model_update_ignored_models = Array.from(
      new Set(
        String(formData.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean)
      )
    )
    if (
      !Array.isArray(settingsObj.upstream_model_update_last_detected_models) ||
      settingsObj.upstream_model_update_check_enabled !== true
    ) {
      settingsObj.upstream_model_update_last_detected_models = []
    }
    if (typeof settingsObj.upstream_model_update_last_check_time !== 'number') {
      settingsObj.upstream_model_update_last_check_time = 0
    }
  } else {
    // These fields are meaningful only when the channel exposes a model-list
    // endpoint. Clear stale values when an Advanced Custom route is removed
    // (or the channel type changes), otherwise the backend will reject the
    // saved configuration as an enabled sync without /v1/models.
    delete settingsObj.upstream_model_update_check_enabled
    delete settingsObj.upstream_model_update_auto_sync_enabled
    delete settingsObj.upstream_model_update_ignored_models
    delete settingsObj.upstream_model_update_last_check_time
    delete settingsObj.upstream_model_update_last_detected_models
    delete settingsObj.upstream_model_update_last_removed_models
  }

  const ratioSyncAccessToken = formData.ratio_sync_access_token?.trim()
  if (ratioSyncAccessToken) {
    settingsObj.ratio_sync_access_token = ratioSyncAccessToken
  } else {
    delete settingsObj.ratio_sync_access_token
  }
  if ((formData.ratio_sync_user_id || 0) > 0) {
    settingsObj.ratio_sync_user_id = formData.ratio_sync_user_id
  } else {
    delete settingsObj.ratio_sync_user_id
  }

  return JSON.stringify(settingsObj)
}

/**
 * Transform form data to API payload for creating channel
 */
export function transformFormDataToCreatePayload(formData: ChannelFormValues): {
  mode: 'single' | 'batch' | 'multi_to_single'
  multi_key_mode?: 'random' | 'polling'
  batch_add_set_key_prefix_2_name?: boolean
  channel: Partial<Channel>
} {
  const mode = formData.multi_key_mode || 'single'

  const channel: Partial<Channel> = {
    name: formData.name,
    type: formData.type,
    base_url: formData.base_url || null,
    key: formData.key,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority || null,
    weight: formData.weight || null,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  // Clean up empty strings to null for optional fields
  Object.keys(channel).forEach((key) => {
    if (channel[key as keyof typeof channel] === '') {
      ;(channel as Record<string, unknown>)[key] = null
    }
  })

  return {
    mode,
    multi_key_mode:
      mode === 'multi_to_single' ? formData.multi_key_type : undefined,
    batch_add_set_key_prefix_2_name:
      mode === 'batch' ? formData.batch_add_set_key_prefix_2_name : undefined,
    channel,
  }
}

/**
 * Transform form data to API payload for updating channel
 */
export function transformFormDataToUpdatePayload(
  formData: ChannelFormValues,
  channelId: number,
  baselineStatus?: number
): Partial<Channel> {
  const payload: Partial<Channel> = {
    id: channelId,
    name: formData.name,
    type: formData.type,
    base_url: formData.base_url || null,
    openai_organization: formData.openai_organization || null,
    models: formData.models,
    group: formatGroups(formData.group),
    model_mapping: formData.model_mapping || null,
    priority: formData.priority ?? 0,
    weight: formData.weight ?? 0,
    test_model: formData.test_model || null,
    auto_ban: formData.auto_ban ?? 1,
    status: formData.status,
    status_code_mapping: formData.status_code_mapping || null,
    tag: formData.tag || null,
    remark: formData.remark || '',
    setting: buildSettingJSON(formData),
    param_override: formData.param_override || null,
    header_override: formData.header_override || null,
    settings: buildSettingsJSON(formData),
    other: formData.other || '',
  }

  if (baselineStatus !== undefined && formData.status === baselineStatus) {
    delete payload.status
  }

  // Only include key if it was changed (not empty)
  if (formData.key && formData.key.trim()) {
    payload.key = formData.key
  }

  // Clean up empty strings to null for optional fields
  Object.keys(payload).forEach((key) => {
    if (payload[key as keyof typeof payload] === '') {
      ;(payload as Record<string, unknown>)[key] = null
    }
  })

  // Send explicit empty strings for nullable fields so GORM updates can clear them.
  payload.base_url = formData.base_url || ''
  payload.openai_organization = formData.openai_organization || ''
  payload.test_model = formData.test_model || ''
  payload.tag = formData.tag || ''
  payload.remark = formData.remark || ''
  payload.model_mapping = formData.model_mapping || ''
  payload.status_code_mapping = formData.status_code_mapping || ''
  payload.param_override = formData.param_override || ''
  payload.header_override = formData.header_override || ''

  return payload
}

// ============================================================================
// Validation Helpers
// ============================================================================

/**
 * Validate JSON string
 */
export function validateJSON(value: string): boolean {
  if (!value || value.trim() === '') return true
  try {
    JSON.parse(value)
    return true
  } catch {
    return false
  }
}

/**
 * Validate model mapping format
 */
export function validateModelMapping(value: string): boolean {
  if (!value || value.trim() === '') return true
  return validateJSON(value)
}

/**
 * Parse models string to array
 */
export function parseModels(models: string): string[] {
  if (!models) return []
  return models
    .split(',')
    .map((m) => m.trim())
    .filter((m) => m.length > 0)
}

/**
 * Parse groups string to array
 */
export function parseGroups(groups: string): string[] {
  if (!groups) return []
  return groups
    .split(',')
    .map((g) => g.trim())
    .filter((g) => g.length > 0)
}

/**
 * Format models array to string
 */
export function formatModels(models: string[]): string {
  return models.join(',')
}

/**
 * Format groups array to string
 */
export function formatGroups(groups: string[]): string {
  return groups.join(',')
}
