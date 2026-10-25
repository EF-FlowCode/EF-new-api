// ----------------------------------------------------------------------------
// Pricing Types
// ----------------------------------------------------------------------------

export type PricingVendor = {
  id: number
  name: string
  icon?: string
  description?: string
}

export type PricingModel = {
  task_pricing?: Record<string, TaskBillingRule[]>
  billing_usage?: BillingUsageInfo
  knowledge?: {
    kind: 'embedding' | 'rerank'
    multimodal: boolean
    batch_file: boolean
    batch_groups?: string[]
    prices?: { input: number; image?: number; batch_input?: number }
  }
  id: number
  model_name: string
  description?: string
  icon?: string
  vendor_id?: number
  vendor_name?: string
  vendor_icon?: string
  vendor_description?: string
  quota_type: number
  model_ratio: number
  completion_ratio: number
  model_price?: number
  cache_ratio?: number | null
  create_cache_ratio?: number | null
  image_ratio?: number | null
  audio_ratio?: number | null
  audio_completion_ratio?: number | null
  enable_groups: string[]
  tags?: string
  supported_endpoint_types?: string[]
  /** Maximum context window in tokens, when configured by the administrator. */
  context_window?: number
  key?: string
  group_ratio?: Record<string, number>
  /** Billing mode (e.g. "tiered_expr") used to flag dynamic pricing */
  billing_mode?: string
  /** Raw expression describing dynamic / tiered billing */
  billing_expr?: string
  /** Structured per-unit pricing whose rate varies by a request dimension. */
  dimensional_pricing?: DimensionalPricing
  /** Pricing version returned by backend, useful for cache busting */
  pricing_version?: string
}

export type BillingUsageInfo = {
  plugins?: Record<string, BillingUsageInfo>
  status: 'ready' | 'unavailable' | 'conflict'
  schema?: Record<
    string,
    {
      type?: 'number' | 'boolean'
      unit?: string
      enum?: string[]
      description?: Record<string, string>
    }
  >
  examples?: {
    label: string
    facts: Record<string, string | number | boolean>
  }[]
  sources?: {
    plugin: string
    version: string
    source_hash: string
    model: string
  }[]
}

export type TaskBillingRule = {
  rule?: {
    kind: string
    expression?: string
    unit_price?: number
    model_ratio?: number
    dimensions?: string
    settlement?: string
  }
  source:
    | 'plugin_original'
    | 'plugin_mapped'
    | 'model_original'
    | 'model_mapped'
    | 'legacy_original'
    | 'missing'
    | 'unreviewed_original'
    | 'explicit_task_rule_required'
  plugin: string
  model: string
  mapped_model: string
  expression?: string
  billing_mode?: string
}

export type DimensionalPricingTier = {
  key: string
  label: string
  ratio: number
}

export type DimensionalPricing = {
  unit: 'second' | string
  dimension: 'resolution' | string
  sample_quantity: number
  tiers: DimensionalPricingTier[]
}

export type PricingData = {
  success: boolean
  message?: string
  data: PricingModel[]
  vendors: PricingVendor[]
  group_ratio: Record<string, number>
  usable_group: Record<string, string>
  supported_endpoint: Record<string, string>
  group_endpoint_overrides?: Record<string, PricingEndpointOverride>
  auto_groups: string[]
}

export type PricingEndpointOverride = {
  type?: string
  path: string
  method?: string
}

export type TokenUnit = 'M' | 'K'
export type PriceType =
  | 'input'
  | 'output'
  | 'cache'
  | 'create_cache'
  | 'image'
  | 'audio_input'
  | 'audio_output'
export type QuotaType = 0 | 1 // 0: token-based, 1: per-request
