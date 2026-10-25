import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  getRequestErrorMessage,
  type ApiResult,
  unwrapApiResult,
} from '@/lib/api'
import type {
  ChannelStat,
  GroupDetail,
  GroupListResult,
  GroupMonitoringSettingsPatch,
  GroupSummary,
  HistoryResponse,
  MonitoringModelsResult,
} from './types'

export async function fetchMonitoringModels(
  admin: boolean,
  signal?: AbortSignal
) {
  const prefix = admin ? 'admin' : 'public'
  const res = await api.get<ApiResult<MonitoringModelsResult>>(
    `/api/monitoring/${prefix}/models`,
    { ...CALLER_HANDLED_REQUEST_CONFIG, signal }
  )
  return unwrapApiResult(res.data, 'Failed to load model monitoring data')
}

type GroupApiResult<T> = ApiResult<T> & {
  // The history endpoint sometimes returns an extra top-level field.
  aggregation_interval_minutes?: number
  all_groups?: string[]
  monitored_groups?: string[]
  categories?: Record<string, string>
  category_order?: string[]
}

// eflow: 同时返回 categories / category_order 等元数据。dashboard 需要这些来按
// 分类块渲染卡片；admin 视图额外拿到 all_groups + monitored_groups 来填管理 drawer。
export async function fetchGroups(admin: boolean): Promise<GroupListResult> {
  const prefix = admin ? 'admin' : 'public'
  const res = await api.get<GroupApiResult<GroupSummary[] | null>>(
    `/api/monitoring/${prefix}/groups`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  const groups =
    unwrapApiResult(res.data, 'Failed to fetch monitoring groups') ?? []
  return {
    groups,
    meta: {
      all_groups: res.data.all_groups,
      monitored_groups: res.data.monitored_groups,
      categories: res.data.categories || {},
      category_order: res.data.category_order || [],
    },
  }
}

// eflow: 以 admin 身份原子保存监控分组管理（哪些分组在监控列表 + 类别 + 类别顺序）。
// 只写这三个子键，其余字段（聚合周期 / 排除规则等）天然不受影响。
export async function updateGroupMonitoringSettings(
  patch: GroupMonitoringSettingsPatch
): Promise<void> {
  const updates: Array<{ key: string; value: string }> = [
    {
      key: 'group_monitoring_setting.monitoring_groups',
      value: JSON.stringify(patch.monitoring_groups),
    },
    {
      key: 'group_monitoring_setting.group_categories',
      value: JSON.stringify(patch.group_categories),
    },
    {
      key: 'group_monitoring_setting.category_order',
      value: JSON.stringify(patch.category_order),
    },
  ]
  const res = await api.put<ApiResult<unknown>>(
    '/api/option/bulk',
    { options: updates },
    CALLER_HANDLED_REQUEST_CONFIG
  )
  unwrapApiResult(res.data, 'Failed to save monitoring settings')
}

export async function fetchGroupHistory(
  admin: boolean,
  groupName: string
): Promise<{ history: HistoryResponse['history']; intervalMinutes: number }> {
  const prefix = admin ? 'admin' : 'public'
  const res = await api.get<
    GroupApiResult<HistoryResponse | HistoryResponse['history'] | null>
  >(
    `/api/monitoring/${prefix}/groups/${encodeURIComponent(groupName)}/history`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  if (!res.data.success) {
    return { history: [], intervalMinutes: 5 }
  }
  const raw = unwrapApiResult(res.data, 'Failed to fetch group history')
  // The backend may return either { history, aggregation_interval_minutes }
  // wrapped in `data`, or just the history array.
  const wrapped = raw && !Array.isArray(raw) ? (raw as HistoryResponse) : null
  const history = wrapped
    ? wrapped.history || []
    : (raw as HistoryResponse['history']) || []
  const intervalMinutes =
    wrapped?.aggregation_interval_minutes ??
    res.data.aggregation_interval_minutes ??
    5
  return { history, intervalMinutes }
}

export async function fetchAdminGroupDetail(
  groupName: string
): Promise<GroupDetail | null> {
  // Backend returns channel_stats beside data, not inside it.
  const res = await api.get<
    ApiResult<GroupDetail | null> & { channel_stats?: ChannelStat[] }
  >(
    `/api/monitoring/admin/groups/${encodeURIComponent(groupName)}`,
    CALLER_HANDLED_REQUEST_CONFIG
  )
  const group = unwrapApiResult(res.data, 'Failed to fetch group detail')
  if (!group) return null
  return { ...group, channel_stats: res.data.channel_stats ?? [] }
}

export async function refreshAdminMonitoring(): Promise<boolean> {
  let res
  try {
    res = await api.post<ApiResult<unknown>>(
      '/api/monitoring/admin/refresh',
      undefined,
      CALLER_HANDLED_REQUEST_CONFIG
    )
  } catch (error) {
    // 429/503 carry the reason in the body; surface it instead of axios' text.
    throw new Error(
      getRequestErrorMessage(error, 'Failed to refresh monitoring data'),
      { cause: error }
    )
  }
  unwrapApiResult(res.data, 'Failed to refresh monitoring data')
  return true
}
