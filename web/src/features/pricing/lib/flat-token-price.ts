import type { PricingModel } from '../types'
import { BILLING_PRICING_VARS, parseFlatTokenBillingExpr } from './billing-expr'
import {
  formatDynamicUnitPrice,
  type DynamicPriceEntry,
  type DynamicPriceOptions,
} from './dynamic-price'
import { toAmountExpression } from './expression-units'
import { getEffectivePricingGroup } from './price'

export type FlatTokenPrices = {
  inputPrice: string
  outputPrice: string
  cacheReadPrice: string
  cacheCreatePrice: string
  cacheCreate1hPrice: string
  imageInputPrice?: string
  imageOutputPrice?: string
  audioInputPrice?: string
  audioOutputPrice?: string
  batchInputPrice?: string
}

export const EMPTY_FLAT_TOKEN_PRICES: FlatTokenPrices = {
  inputPrice: '',
  outputPrice: '',
  cacheReadPrice: '',
  cacheCreatePrice: '',
  cacheCreate1hPrice: '',
  imageInputPrice: '',
  imageOutputPrice: '',
  audioInputPrice: '',
  audioOutputPrice: '',
  batchInputPrice: '',
}

const FLAT_PRICE_FIELDS = [
  ['p', 'inputPrice', true],
  ['c', 'outputPrice', true],
  ['cr', 'cacheReadPrice', false],
  ['cc', 'cacheCreatePrice', false],
  ['cc1h', 'cacheCreate1hPrice', false],
  ['img', 'imageInputPrice', false],
  ['img_o', 'imageOutputPrice', false],
  ['ai', 'audioInputPrice', false],
  ['ao', 'audioOutputPrice', false],
] as const

function normalizePrice(value: string): string | null {
  const text = value.trim()
  if (!text) return null
  const number = Number(text)
  if (!Number.isFinite(number) || number < 0) return null
  return String(number)
}

export function buildFlatTokenPriceExpr(
  prices: FlatTokenPrices
): string | null {
  const terms: string[] = []
  for (const [variable, field, required] of FLAT_PRICE_FIELDS) {
    const raw = prices[field] || ''
    const normalized = normalizePrice(raw)
    if (normalized === null) {
      if (required || raw.trim() !== '') return null
      continue
    }
    terms.push(`${variable} * ${normalized}`)
  }
  return toAmountExpression(`tier("default", ${terms.join(' + ')})`)
}

export function parseFlatTokenPriceExpr(
  expression: string
): FlatTokenPrices | null {
  const parsed = parseFlatTokenBillingExpr(expression)
  if (!parsed) return null
  const result = { ...EMPTY_FLAT_TOKEN_PRICES }
  for (const [variable, field, required] of FLAT_PRICE_FIELDS) {
    const value = parsed.coefficients[variable]
    if (value === undefined) {
      if (required) return null
      continue
    }
    if (!Number.isFinite(value) || value < 0) return null
    result[field] = String(value)
  }
  return result
}

export function isFlatTokenPriceModel(model: PricingModel): boolean {
  if (
    model.quota_type === 0 &&
    (!model.billing_mode || model.billing_mode === 'ratio')
  )
    return true
  if (
    model.knowledge?.prices &&
    (!model.billing_mode || model.billing_mode === 'ratio')
  )
    return true
  if (
    model.knowledge &&
    model.quota_type === 0 &&
    (!model.billing_mode || model.billing_mode === 'ratio')
  )
    return true
  if (
    (model.billing_mode !== 'flat_price' &&
      model.billing_mode !== 'tiered_expr') ||
    !model.billing_expr
  ) {
    return false
  }
  return parseFlatTokenPriceExpr(model.billing_expr) !== null
}

export function hasKnowledgeBatchPrice(
  model: PricingModel,
  selectedGroup?: string
): boolean {
  const group = getEffectivePricingGroup(model, selectedGroup)
  return Boolean(
    model.knowledge?.batch_file &&
    model.knowledge.prices?.batch_input !== undefined &&
    group &&
    model.knowledge.batch_groups?.includes(group)
  )
}

export function getFlatTokenPriceEntries(
  model: PricingModel,
  options: DynamicPriceOptions
): DynamicPriceEntry[] {
  if (!isFlatTokenPriceModel(model)) return []
  const prices =
    !model.billing_mode || model.billing_mode === 'ratio'
      ? ratioToFlatTokenPrices({
          ratio: String(model.model_ratio),
          completionRatio: String(model.completion_ratio),
          cacheRatio: model.cache_ratio?.toString(),
          createCacheRatio: model.create_cache_ratio?.toString(),
          imageRatio: model.image_ratio?.toString(),
          audioRatio: model.audio_ratio?.toString(),
          audioCompletionRatio: model.audio_completion_ratio?.toString(),
        })
      : parseFlatTokenPriceExpr(model.billing_expr || '')
  if (!prices) return []

  const entries = FLAT_PRICE_FIELDS.flatMap(([variableKey, field]) => {
    const rawValue = prices[field]
    if (
      !rawValue ||
      (model.knowledge && variableKey === 'c' && Number(rawValue) === 0)
    )
      return []
    const value = Number(rawValue)
    const variable = BILLING_PRICING_VARS.find(
      (item) => item.key === variableKey
    )
    if (!variable?.field || !Number.isFinite(value)) return []
    return [
      {
        key: variable.key,
        field: variable.field,
        label: variable.label,
        shortLabel: variable.shortLabel,
        value,
        formatted: formatDynamicUnitPrice(value, options),
        variable,
      },
    ]
  })
  if (hasKnowledgeBatchPrice(model, options.selectedGroup)) {
    const value = model.knowledge!.prices!.batch_input!
    const variable = BILLING_PRICING_VARS.find((item) => item.key === 'p')!
    entries.push({
      key: 'batch',
      field: 'batchInputPrice',
      label: 'Batch input price',
      shortLabel: 'Batch input price',
      value,
      formatted: formatDynamicUnitPrice(value, options),
      variable,
    })
  }
  return entries
}

// Audio output is relative to audio input, not text input. Missing optional
// dimensions stay empty; absence and a configured zero must remain distinct.
export function ratioToFlatTokenPrices(data: {
  ratio?: string
  completionRatio?: string
  cacheRatio?: string
  createCacheRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
  knowledgeBatchInput?: string
}): FlatTokenPrices {
  // Display conversion only. No-op editing leaves original coefficients intact.
  const display = (value: number) => String(Number(value.toPrecision(12)))
  const input = data.ratio ? Number(data.ratio) * 2 : undefined
  const relative = (raw?: string, base = input) =>
    raw !== undefined && raw !== '' && base !== undefined
      ? display(Number(raw) * base)
      : ''
  return {
    ...EMPTY_FLAT_TOKEN_PRICES,
    inputPrice: input === undefined ? '' : display(input),
    outputPrice:
      input === undefined
        ? ''
        : display(input * Number(data.completionRatio || 1)),
    cacheReadPrice: relative(data.cacheRatio),
    cacheCreatePrice: relative(data.createCacheRatio),
    imageInputPrice: relative(data.imageRatio),
    audioInputPrice: relative(data.audioRatio),
    audioOutputPrice: relative(
      data.audioCompletionRatio,
      input === undefined ? undefined : input * Number(data.audioRatio || 1)
    ),
    batchInputPrice: data.knowledgeBatchInput || '',
  }
}
