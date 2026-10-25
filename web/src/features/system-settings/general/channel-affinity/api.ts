import { api, CALLER_HANDLED_REQUEST_CONFIG } from '@/lib/api'
import type { CacheStats } from './types'

export async function getCacheStats(): Promise<{
  success: boolean
  message?: string
  data?: CacheStats
}> {
  const res = await api.get('/api/option/channel_affinity_cache', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    disableDuplicate: true,
  })
  return res.data
}

export async function clearAllCache(): Promise<{
  success: boolean
  message?: string
}> {
  const res = await api.delete('/api/option/channel_affinity_cache', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params: { all: true },
  })
  return res.data
}

export async function clearRuleCache(
  ruleName: string
): Promise<{ success: boolean; message?: string }> {
  const res = await api.delete('/api/option/channel_affinity_cache', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params: { rule_name: ruleName },
  })
  return res.data
}

export async function clearGroupCache(
  group: string
): Promise<{ success: boolean; message?: string; data?: { deleted: number } }> {
  const res = await api.delete('/api/option/channel_affinity_cache', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params: { group },
  })
  return res.data
}

export async function getAffinityUsageCache(params: {
  rule_name: string
  using_group: string
  key_hint: string
  key_fp: string
}): Promise<{ success: boolean; message?: string; data?: unknown }> {
  const res = await api.get('/api/log/channel_affinity_usage_cache', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    params,
    disableDuplicate: true,
  })
  return res.data
}
