import {
  SORT_OPTIONS,
  FILTER_ALL,
  QUOTA_TYPES,
  QUOTA_TYPE_VALUES,
  ENDPOINT_TYPES,
  type SortOption,
} from '../constants'
import type { PricingEndpointOverride, PricingModel } from '../types'
import { isDimensionalPricingModel } from './dimensional-price'
import { getDynamicPricingSummary } from './dynamic-price'
import {
  getFlatTokenPriceEntries,
  isFlatTokenPriceModel,
} from './flat-token-price'
import { getEffectiveEndpointTypes, isTokenBasedModel } from './model-helpers'

// ----------------------------------------------------------------------------
// Filter Utilities
// ----------------------------------------------------------------------------

/**
 * Filter models by search query
 */
export function filterBySearch(
  models: PricingModel[],
  query: string
): PricingModel[] {
  if (!query) return models

  const lowerQuery = query.toLowerCase()
  return models.filter(
    (m) =>
      m.model_name?.toLowerCase().includes(lowerQuery) ||
      m.description?.toLowerCase().includes(lowerQuery) ||
      m.tags?.toLowerCase().includes(lowerQuery) ||
      m.vendor_name?.toLowerCase().includes(lowerQuery)
  )
}

/**
 * Filter models by vendor
 */
export function filterByVendor(
  models: PricingModel[],
  vendor: string
): PricingModel[] {
  if (vendor === FILTER_ALL) return models
  return models.filter((m) => m.vendor_name === vendor)
}

/**
 * Filter models by group
 */
export function filterByGroup(
  models: PricingModel[],
  group: string
): PricingModel[] {
  if (group === FILTER_ALL) return models
  return models.filter((m) => m.enable_groups?.includes(group))
}

/**
 * Filter models by quota type
 */
export function filterByQuotaType(
  models: PricingModel[],
  quotaType: string
): PricingModel[] {
  if (quotaType === QUOTA_TYPES.ALL) return models
  const targetType =
    quotaType === QUOTA_TYPES.TOKEN
      ? QUOTA_TYPE_VALUES.TOKEN
      : QUOTA_TYPE_VALUES.REQUEST
  return models.filter((m) => m.quota_type === targetType)
}

/**
 * Filter models by endpoint type
 */
export function filterByEndpointType(
  models: PricingModel[],
  endpointType: string,
  group: string,
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
): PricingModel[] {
  if (endpointType === ENDPOINT_TYPES.ALL) return models
  return models.filter((model) =>
    getEffectiveEndpointTypes(model, groupEndpointOverrides, group).includes(
      endpointType
    )
  )
}

export function countModelsByEndpointType(
  models: PricingModel[],
  endpointType: string,
  group: string,
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
): number {
  if (endpointType === ENDPOINT_TYPES.ALL) return models.length
  return models.filter((model) =>
    getEffectiveEndpointTypes(model, groupEndpointOverrides, group).includes(
      endpointType
    )
  ).length
}

// eflow: 按上游 c9f7b6208 —— 价格排序分输入 / 输出 / 按次三种，取卡片上展示的
// 基础单价（美元，token 价按每 1M，不乘分组倍率与时段倍率）比较；某种价格不存在的
// 模型（如按次模型的输入价、表达式模型的按次价、按秒计费）无论升降序都排在最后。
type SortPriceKind = 'input' | 'output' | 'request'

const PRICE_SORTS: Partial<
  Record<SortOption, { kind: SortPriceKind; dir: 1 | -1 }>
> = {
  [SORT_OPTIONS.INPUT_PRICE_LOW]: { kind: 'input', dir: 1 },
  [SORT_OPTIONS.INPUT_PRICE_HIGH]: { kind: 'input', dir: -1 },
  [SORT_OPTIONS.OUTPUT_PRICE_LOW]: { kind: 'output', dir: 1 },
  [SORT_OPTIONS.OUTPUT_PRICE_HIGH]: { kind: 'output', dir: -1 },
  [SORT_OPTIONS.REQUEST_PRICE_LOW]: { kind: 'request', dir: 1 },
  [SORT_OPTIONS.REQUEST_PRICE_HIGH]: { kind: 'request', dir: -1 },
}

function finitePrice(value: number | undefined): number | undefined {
  return value !== undefined && Number.isFinite(value) ? value : undefined
}

export function getSortPrice(
  model: PricingModel,
  kind: SortPriceKind
): number | undefined {
  const dynamic = getDynamicPricingSummary(model, { tokenUnit: 'M' })
  if (kind === 'request') {
    if (
      dynamic ||
      isTokenBasedModel(model) ||
      isDimensionalPricingModel(model)
    ) {
      return undefined
    }
    return finitePrice(model.model_price)
  }

  const field = kind === 'input' ? 'inputPrice' : 'outputPrice'
  if (dynamic) {
    return finitePrice(
      dynamic.entries.find((entry) => entry.field === field)?.value
    )
  }
  if (isFlatTokenPriceModel(model)) {
    return finitePrice(
      getFlatTokenPriceEntries(model, { tokenUnit: 'M' }).find(
        (entry) => entry.field === field
      )?.value
    )
  }
  if (!isTokenBasedModel(model) || model.billing_mode === 'audio_seconds') {
    return undefined
  }
  return finitePrice(
    model.model_ratio * 2 * (kind === 'output' ? model.completion_ratio : 1)
  )
}

/**
 * Sort models by specified option
 */
export function sortModels(
  models: PricingModel[],
  sortBy: string
): PricingModel[] {
  const sorted = [...models]

  const priceSort = PRICE_SORTS[sortBy as SortOption]
  if (priceSort) {
    // 取价要解析计费表达式，先每个模型算一次。
    const price = new Map(
      sorted.map((m) => [m, getSortPrice(m, priceSort.kind)])
    )
    sorted.sort((a, b) => {
      const priceA = price.get(a)
      const priceB = price.get(b)
      if (priceA === undefined || priceB === undefined) {
        return Number(priceA === undefined) - Number(priceB === undefined)
      }
      return priceSort.dir * (priceA - priceB)
    })
    return sorted
  }

  if (sortBy === SORT_OPTIONS.NAME) {
    sorted.sort((a, b) =>
      (a.model_name || '').localeCompare(b.model_name || '')
    )
  }

  return sorted
}

/**
 * Apply all filters and sorting to models
 */
export function filterAndSortModels(
  models: PricingModel[],
  filters: {
    search: string
    vendor: string
    group: string
    quotaType: string
    endpointType: string
    tag: string
    sortBy: string
    groupEndpointOverrides?: Record<string, PricingEndpointOverride>
  }
): PricingModel[] {
  let result = filterBySearch(models, filters.search)
  result = filterByVendor(result, filters.vendor)
  result = filterByGroup(result, filters.group)
  result = filterByQuotaType(result, filters.quotaType)
  result = filterByEndpointType(
    result,
    filters.endpointType,
    filters.group,
    filters.groupEndpointOverrides
  )
  result = filterByTag(result, filters.tag)
  result = sortModels(result, filters.sortBy)

  return result
}

/**
 * Parse tags from comma-separated string
 */
export function parseTags(tagsString?: string): string[] {
  if (!tagsString) return []
  return tagsString
    .split(/[,;|\s]+/)
    .map((t) => t.trim())
    .filter(Boolean)
}

/**
 * Extract all unique tags from models
 */
export function extractAllTags(models: PricingModel[]): string[] {
  const tagSet = new Set<string>()

  models.forEach((model) => {
    if (model.tags) {
      const tags = parseTags(model.tags)
      tags.forEach((tag) => {
        tagSet.add(tag.toLowerCase())
      })
    }
  })

  return Array.from(tagSet).sort((a, b) => a.localeCompare(b))
}

/**
 * Filter models by tag
 */
export function filterByTag(
  models: PricingModel[],
  tag: string
): PricingModel[] {
  if (tag === FILTER_ALL) return models

  const tagLower = tag.toLowerCase()
  return models.filter((m) => {
    if (!m.tags) return false
    const modelTags = parseTags(m.tags).map((t) => t.toLowerCase())
    return modelTags.includes(tagLower)
  })
}
