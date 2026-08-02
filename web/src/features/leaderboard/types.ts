export type LeaderboardRange = 'today' | 'week' | 'month' | 'year' | 'all'

export interface ModelRankingEntry {
  rank: number
  model_name: string
  token_used: number
  count: number
  quota: number
  trend_pct: number
  trend_known: boolean
}

export interface ModelRankingSeriesPoint {
  time: number
  model_name: string
  token_used: number
}

// 名次走势点——后端逐时间桶给出的名次(1..N),不含任何用量。
export interface ModelRankingRankPoint {
  time: number
  model_name: string
  rank: number
}

export interface ModelRankingPayload {
  range: LeaderboardRange
  bucket: 'hour' | 'day'
  total_tokens: number
  generated_at: number
  // 区间元数据：统计区间起点 / 涨跌对比上一区间起点（unix 秒，0 表示全部时间/无对比）。
  window_start: number
  prev_start: number
  ranking: ModelRankingEntry[]
  series: ModelRankingSeriesPoint[]
  rank_series: ModelRankingRankPoint[]
}
