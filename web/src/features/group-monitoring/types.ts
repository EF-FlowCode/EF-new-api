// ----------------------------------------------------------------------------
// Group monitoring types — mirrors backend /api/monitoring response shapes
// ----------------------------------------------------------------------------

export type HistoryPoint = {
  recorded_at: number
  availability_rate?: number | null
  cache_hit_rate?: number | null
}

export type GroupSummary = {
  group_name: string
  is_online?: boolean
  online_channels?: number
  total_channels?: number
  availability_rate?: number | null
  cache_hit_rate?: number | null
  avg_frt?: number | null
  first_response_time?: number | null
  last_test_model?: string
  group_ratio?: number | null
  updated_at?: number
  history?: HistoryPoint[]
  aggregation_interval_minutes?: number
}

export type ChannelStat = {
  channel_id: number
  channel_name?: string
  enabled?: boolean
  availability_rate?: number | null
  cache_hit_rate?: number | null
  first_response_time?: number | null
  test_model?: string
  last_test_time?: number | string | null
}

export type GroupDetail = GroupSummary & {
  channel_stats?: ChannelStat[]
}

export type HistoryResponse = {
  history?: HistoryPoint[]
  aggregation_interval_minutes?: number
}

// eflow: 后端 admin / public 接口除了 data 之外还回传分类信息（all_groups 仅
// admin 才有，category_order 两边都有，让公共 status 页也按同一分类块渲染）。
export type GroupListMeta = {
  /** Admin 视图下系统里所有分组（含未启用监控的）。Public 视图下不会返回。 */
  all_groups?: string[]
  /** 当前 monitoring_groups 列表，仅 admin 视图返回，用于"显示开关"的初始勾选状态。 */
  monitored_groups?: string[]
  /** groupName → categoryName。未出现的视为"未分类"。 */
  categories: Record<string, string>
  /** 分类块在 UI 上的渲染顺序。不在列表里的分类按字母回退、"未分类"始终最后。 */
  category_order: string[]
}

export type GroupListResult = {
  groups: GroupSummary[]
  meta: GroupListMeta
}

export type GroupMonitoringSettingsPatch = {
  monitoring_groups: string[]
  group_categories: Record<string, string>
  category_order: string[]
}
