import type { RatioType } from '../types'
import { RATIO_TYPE_OPTIONS } from './constants'

export type RatioDifferenceEntry = {
  current: number | string | null
  upstreams: Record<string, number | string | 'same'>
  confidence: Record<string, boolean>
}

export type ModelRow = {
  key: string
  model: string
  ratioTypes: Partial<Record<RatioType, RatioDifferenceEntry>>
  billingConflict: boolean
}

export type ResolutionsMap = Record<string, Record<string, number | string>>

export const RATIO_SYNC_FIELDS: RatioType[] = [
  'model_ratio',
  'completion_ratio',
  'cache_ratio',
  'create_cache_ratio',
  'image_ratio',
  'batch_input_ratio',
  'audio_ratio',
  'audio_completion_ratio',
]

export const SYNC_FIELD_ORDER: RatioType[] = [
  ...RATIO_SYNC_FIELDS,
  'model_price',
  'billing_mode',
  'billing_expr',
]

export const NUMERIC_SYNC_FIELDS = new Set<string>([
  ...RATIO_SYNC_FIELDS,
  'model_price',
])

export const PRICING_OPTION_KEYS = [
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'BatchInputRatio',
  'AudioRatio',
  'AudioCompletionRatio',
  'ModelPrice',
  'billing_setting.billing_mode',
  'billing_setting.billing_expr',
] as const

const RATIO_OPTION_KEYS = [
  'ModelRatio',
  'CompletionRatio',
  'CacheRatio',
  'CreateCacheRatio',
  'ImageRatio',
  'BatchInputRatio',
  'AudioRatio',
  'AudioCompletionRatio',
] as const

const EXPRESSION_BILLING_MODES = new Set(['flat_price', 'tiered_expr'])

export type PricingOptionKey = (typeof PRICING_OPTION_KEYS)[number]
export type PricingOptionRecords = Record<
  PricingOptionKey,
  Record<string, number | string>
>
export type BillingCategory = 'price' | 'ratio' | 'expression'

export function getBillingCategory(ratioType: string): BillingCategory {
  if (ratioType === 'model_price') return 'price'
  if (ratioType === 'billing_mode' || ratioType === 'billing_expr')
    return 'expression'
  return 'ratio'
}

export function getResolutionBillingCategory(
  resolution: Record<string, number | string>
): BillingCategory | null {
  const fields = Object.keys(resolution)
  if (fields.some((field) => getBillingCategory(field) === 'expression'))
    return 'expression'
  if (fields.includes('model_price')) return 'price'
  if (fields.some((field) => getBillingCategory(field) === 'ratio'))
    return 'ratio'
  return null
}

export function getConfiguredBillingCategory(
  model: string,
  options: PricingOptionRecords
): BillingCategory | null {
  const mode = options['billing_setting.billing_mode'][model]
  if (EXPRESSION_BILLING_MODES.has(String(mode))) {
    return 'expression'
  }
  if (options.ModelPrice[model] !== undefined) return 'price'
  if (
    RATIO_OPTION_KEYS.some(
      (optionKey) => options[optionKey][model] !== undefined
    )
  ) {
    return 'ratio'
  }
  return null
}

export function optionKeyBySyncField(ratioType: string): PricingOptionKey {
  const explicit: Record<string, PricingOptionKey> = {
    billing_mode: 'billing_setting.billing_mode',
    billing_expr: 'billing_setting.billing_expr',
  }
  if (explicit[ratioType]) return explicit[ratioType]
  return ratioType
    .split('_')
    .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
    .join('') as PricingOptionKey
}

function clearExpressionBilling(options: PricingOptionRecords, model: string) {
  const mode = options['billing_setting.billing_mode'][model]
  if (EXPRESSION_BILLING_MODES.has(String(mode))) {
    delete options['billing_setting.billing_mode'][model]
  }
  delete options['billing_setting.billing_expr'][model]
}

export function applyPricingSyncResolutions(
  current: PricingOptionRecords,
  resolutions: ResolutionsMap
): PricingOptionRecords {
  const next = Object.fromEntries(
    PRICING_OPTION_KEYS.map((key) => [key, { ...current[key] }])
  ) as PricingOptionRecords

  Object.entries(resolutions).forEach(([model, resolution]) => {
    const category = getResolutionBillingCategory(resolution)
    if (!category) return

    if (category === 'expression') {
      RATIO_OPTION_KEYS.forEach((key) => delete next[key][model])
      delete next.ModelPrice[model]
    } else {
      clearExpressionBilling(next, model)
      if (category === 'price') {
        RATIO_OPTION_KEYS.forEach((key) => delete next[key][model])
      } else {
        delete next.ModelPrice[model]
      }
    }

    Object.entries(resolution).forEach(([ratioType, value]) => {
      if (getBillingCategory(ratioType) !== category) return
      const optionKey = optionKeyBySyncField(ratioType)
      next[optionKey][model] = NUMERIC_SYNC_FIELDS.has(ratioType)
        ? Number(value)
        : value
    })
  })

  return next
}

export function buildPricingSyncUpdates(
  current: PricingOptionRecords,
  resolutions: ResolutionsMap
) {
  const next = applyPricingSyncResolutions(current, resolutions)
  return PRICING_OPTION_KEYS.map((key) => ({
    key,
    value: JSON.stringify(next[key], null, 2),
  }))
}

export function getSyncFieldLabel(
  ratioType: string,
  t: (key: string) => string
): string {
  const opt = RATIO_TYPE_OPTIONS.find((o) => o.value === ratioType)
  if (opt) return t(opt.label)
  return ratioType
}

export function getOrderedRatioTypes(
  ratioTypes: Partial<Record<RatioType, RatioDifferenceEntry>>,
  filter?: string
): RatioType[] {
  const keys = Object.keys(ratioTypes) as RatioType[]
  const ordered = [
    ...SYNC_FIELD_ORDER.filter((f) => keys.includes(f)),
    ...keys.filter((f) => !SYNC_FIELD_ORDER.includes(f)),
  ]
  if (!filter || filter === '__all__') return ordered
  return ordered.filter((f) => f === filter)
}

export function getPreferredSyncField(
  ratioTypes: Partial<Record<RatioType, RatioDifferenceEntry>>,
  ratioType: RatioType,
  sourceName: string
): RatioType {
  const exprValue = ratioTypes.billing_expr?.upstreams?.[sourceName]
  if (
    ratioType !== 'billing_expr' &&
    exprValue !== null &&
    exprValue !== undefined &&
    exprValue !== 'same'
  ) {
    return 'billing_expr'
  }
  return ratioType
}

export function isSelectableUpstreamValue(
  value: number | string | 'same' | null | undefined
): boolean {
  return value !== null && value !== undefined && value !== 'same'
}
