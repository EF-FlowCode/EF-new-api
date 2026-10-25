import { api, type ApiRequestConfig } from '@/lib/api'
import type { PricingData } from './types'

// ----------------------------------------------------------------------------
// Pricing APIs
// ----------------------------------------------------------------------------

// Get model pricing data
export async function getPricing(
  config?: ApiRequestConfig
): Promise<PricingData> {
  const res = await api.get('/api/pricing', config)
  return res.data
}
