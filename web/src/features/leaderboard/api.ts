import { api } from '@/lib/api'
import type { LeaderboardRange, ModelRankingPayload } from './types'

export async function getModelRanking(range: LeaderboardRange): Promise<{
  success: boolean
  message?: string
  data?: ModelRankingPayload
}> {
  const res = await api.get('/api/model_ranking', {
    params: { range },
    disableDuplicate: true,
  } as Record<string, unknown>)
  return res.data
}
