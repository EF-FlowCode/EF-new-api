import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import type { PricingModel } from '../types'

export type DimensionalPricingOptions = {
  groupRatioMultiplier?: number
  showRechargePrice?: boolean
  priceRate?: number
  usdExchangeRate?: number
}

export type DimensionalPricingRow = {
  key: string
  label: string
  ratio: number
  unitPrice: number
  samplePrice: number
  formattedUnitPrice: string
  formattedSamplePrice: string
}

export type DimensionalPricingSummary = {
  unit: string
  dimension: string
  sampleQuantity: number
  rows: DimensionalPricingRow[]
}

export function isDimensionalPricingModel(model: PricingModel): boolean {
  return (
    model.billing_mode === 'dimensional' &&
    Boolean(model.dimensional_pricing?.tiers?.length) &&
    Number(model.model_price) > 0
  )
}

function applyRechargeRate(
  price: number,
  showRechargePrice: boolean,
  priceRate: number,
  usdExchangeRate: number
): number {
  if (!showRechargePrice) return price
  return (price * priceRate) / usdExchangeRate
}

function formatPrice(
  price: number,
  options: DimensionalPricingOptions
): string {
  const adjusted = applyRechargeRate(
    price,
    options.showRechargePrice ?? false,
    options.priceRate ?? 1,
    options.usdExchangeRate ?? 1
  )
  return formatBillingCurrencyFromUSD(adjusted, {
    digitsLarge: 2,
    digitsSmall: 4,
    abbreviate: false,
  })
}

export function getDimensionalPricingSummary(
  model: PricingModel,
  options: DimensionalPricingOptions = {}
): DimensionalPricingSummary | null {
  if (!isDimensionalPricingModel(model)) return null

  const pricing = model.dimensional_pricing!
  const groupRatio = options.groupRatioMultiplier ?? 1
  const sampleQuantity = Number(pricing.sample_quantity) || 1
  const basePrice = Number(model.model_price) * groupRatio
  const rows = pricing.tiers
    .filter((tier) => Number.isFinite(Number(tier.ratio)) && tier.ratio > 0)
    .map((tier) => {
      const unitPrice = basePrice * Number(tier.ratio)
      const samplePrice = unitPrice * sampleQuantity
      return {
        key: tier.key,
        label: tier.label || tier.key,
        ratio: Number(tier.ratio),
        unitPrice,
        samplePrice,
        formattedUnitPrice: formatPrice(unitPrice, options),
        formattedSamplePrice: formatPrice(samplePrice, options),
      }
    })

  if (rows.length === 0) return null
  return {
    unit: pricing.unit,
    dimension: pricing.dimension,
    sampleQuantity,
    rows,
  }
}
