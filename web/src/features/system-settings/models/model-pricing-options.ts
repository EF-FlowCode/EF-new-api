import { combineBillingExpr } from '@/features/pricing/lib/billing-expr'
import type { ModelRatioData } from './model-ratio-dialog'

export const PRICING_OPTION_KEYS = [
  'ModelPrice',
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'AudioRatio',
  'AudioCompletionRatio',
  'BatchInputRatio',
  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
  'billing_setting.task_expressions',
] as const
type Settings = Partial<Record<(typeof PRICING_OPTION_KEYS)[number], string>>

export function readModelPricing(
  settings: Settings,
  name: string,
  completionRatioMeta?: string
): ModelRatioData {
  const read = (key: (typeof PRICING_OPTION_KEYS)[number]) =>
    JSON.parse(settings[key] || '{}')[name]
  const str = (key: (typeof PRICING_OPTION_KEYS)[number]) =>
    read(key)?.toString() || ''
  const mode = read('billing_setting.billing_mode')
  return {
    name,
    price: str('ModelPrice'),
    ratio: str('ModelRatio'),
    completionRatio:
      JSON.parse(completionRatioMeta || '{}')[name]?.ratio?.toString() ||
      str('CompletionRatio'),
    cacheRatio: str('CacheRatio'),
    createCacheRatio: str('CreateCacheRatio'),
    imageRatio: str('ImageRatio'),
    audioRatio: str('AudioRatio'),
    audioCompletionRatio: str('AudioCompletionRatio'),
    knowledgeBatchInput:
      read('BatchInputRatio') === undefined
        ? ''
        : String(read('BatchInputRatio') * 2),
    billingMode:
      mode === 'audio_seconds' ||
      mode === 'flat_price' ||
      mode === 'tiered_expr'
        ? mode
        : str('ModelPrice')
          ? 'per-request'
          : 'per-token',
    billingExpr: str('billing_setting.billing_expr'),
  }
}

// Update only the requested model; rename copies every dimension, even inactive
// expressions and auxiliary records. Saving model metadata alone is a no-op.
export function updateModelPricingOptions(
  settings: Settings,
  name: string,
  draft?: ModelRatioData | null,
  oldName?: string
) {
  const maps = Object.fromEntries(
    PRICING_OPTION_KEYS.map((key) => [key, JSON.parse(settings[key] || '{}')])
  )
  if (oldName && oldName !== name) {
    for (const key of PRICING_OPTION_KEYS) {
      if (maps[key][oldName] !== undefined) maps[key][name] = maps[key][oldName]
      delete maps[key][oldName]
    }
  }
  if (draft) {
    for (const key of PRICING_OPTION_KEYS) {
      if (key === 'billing_setting.task_expressions') continue
      if (!draft.preservePricing || key === 'BatchInputRatio')
        delete maps[key][name]
    }
    if (!draft.preservePricing) {
      if (
        draft.billingMode === 'flat_price' ||
        draft.billingMode === 'tiered_expr'
      ) {
        maps['billing_setting.billing_mode'][name] = draft.billingMode
        maps['billing_setting.billing_expr'][name] = combineBillingExpr(
          draft.billingExpr || '',
          draft.requestRuleExpr || ''
        )
      } else if (draft.billingMode === 'audio_seconds') {
        maps['billing_setting.billing_mode'][name] = 'audio_seconds'
        maps.ModelRatio[name] = Number(draft.ratio)
      } else if (draft.price !== undefined && draft.price !== '') {
        maps.ModelPrice[name] = Number(draft.price)
      }
    }
    if (
      draft.knowledgeBatchInput !== undefined &&
      draft.knowledgeBatchInput !== ''
    )
      maps.BatchInputRatio[name] = Number(draft.knowledgeBatchInput) / 2
  }
  return PRICING_OPTION_KEYS.flatMap((key) =>
    JSON.stringify(JSON.parse(settings[key] || '{}')) ===
    JSON.stringify(maps[key])
      ? []
      : [{ key, value: JSON.stringify(maps[key]) }]
  )
}
