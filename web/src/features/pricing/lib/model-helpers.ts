import { EXCLUDED_GROUPS, QUOTA_TYPE_VALUES } from '../constants'
import type { PricingEndpointOverride, PricingModel } from '../types'

// ----------------------------------------------------------------------------
// Model Helper Utilities
// ----------------------------------------------------------------------------

/**
 * Resolve the icon used to represent a model. Explicit model metadata takes
 * precedence; vendor metadata remains the fallback for unconfigured models.
 */
export function resolveModelIcon(model: PricingModel): string | undefined {
  return model.icon || model.vendor_icon
}

/**
 * Get available groups for a model
 */
export function getAvailableGroups(
  model: PricingModel,
  usableGroup: Record<string, string>
): string[] {
  const modelEnableGroups = Array.isArray(model.enable_groups)
    ? model.enable_groups
    : []

  return Object.keys(usableGroup)
    .filter((g) => !EXCLUDED_GROUPS.includes(g))
    .filter((g) => modelEnableGroups.includes(g))
}

/**
 * Replace model placeholder in endpoint path
 */
export function replaceModelInPath(path: string, modelName: string): string {
  return path.replace(/\{model\}/g, modelName)
}

export function resolveEndpointOverride(
  overrides?: Record<string, PricingEndpointOverride>,
  group?: string
): PricingEndpointOverride | undefined {
  const configured = (group && overrides?.[group]) || overrides?.default
  return configured
}

export const DEFAULT_PRICING_ENDPOINT: Required<PricingEndpointOverride> = {
  type: 'openai',
  path: '/v1/chat/completions',
  method: 'POST',
}

export function resolveModelEndpointOverride(
  model: PricingModel,
  overrides?: Record<string, PricingEndpointOverride>,
  group?: string
): PricingEndpointOverride | undefined {
  if (model.knowledge) return undefined
  const groupOverride = group ? overrides?.[group] : undefined
  if (groupOverride) return groupOverride

  if ((model.supported_endpoint_types || []).length > 0) return undefined
  return overrides?.default ?? DEFAULT_PRICING_ENDPOINT
}

/**
 * Resolve endpoint types without letting the display fallback hide known
 * model capabilities. Explicit group overrides still take precedence;
 * models without endpoint metadata fall back to OpenAI Chat.
 */
export function getEffectiveEndpointTypes(
  model: PricingModel,
  overrides?: Record<string, PricingEndpointOverride>,
  group?: string
): string[] {
  if (model.knowledge)
    return [model.knowledge.kind === 'embedding' ? 'embeddings' : 'jina-rerank']
  const endpoints = model.supported_endpoint_types || []
  const groupOverride = group ? overrides?.[group] : undefined
  if (groupOverride?.type) return [groupOverride.type]
  if (endpoints.length > 0) return endpoints

  const fallback = overrides?.default ?? DEFAULT_PRICING_ENDPOINT
  return [fallback.type || DEFAULT_PRICING_ENDPOINT.type]
}

/**
 * Check if model is token-based pricing
 */
export function isTokenBasedModel(model: PricingModel): boolean {
  return model.quota_type === QUOTA_TYPE_VALUES.TOKEN
}

export function formatContextWindow(tokens?: number): string | null {
  if (!tokens || tokens <= 0) return null
  if (tokens >= 1_000_000) {
    return `${(tokens / 1_000_000).toLocaleString(undefined, { maximumFractionDigits: 1 })}M`
  }
  if (tokens >= 1_000) {
    return `${(tokens / 1_000).toLocaleString(undefined, { maximumFractionDigits: 1 })}K`
  }
  return tokens.toLocaleString()
}
