import { api } from '@/lib/api'
import type {
  GroupDetail,
  GroupListResult,
  GroupMonitoringSettingsPatch,
  GroupSummary,
  HistoryResponse,
} from './types'

type ApiResponse<T> = {
  success: boolean
  message?: string
  data: T
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
  const res = await api.get<ApiResponse<GroupSummary[]>>(
    `/api/monitoring/${prefix}/groups`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to fetch monitoring groups')
  }
  return {
    groups: res.data.data || [],
    meta: {
      all_groups: res.data.all_groups,
      monitored_groups: res.data.monitored_groups,
      categories: res.data.categories || {},
      category_order: res.data.category_order || [],
    },
  }
}

// eflow: 以 admin 身份保存监控分组管理（哪些分组在监控列表 + 类别 + 类别顺序）。
// 按 group_monitoring_setting.<field> 逐键 PUT /api/option/——这是后端
// handleConfigUpdate 唯一认的格式：它按首个 "." 拆 key，把子键 unmarshal 回
// 已注册的 GroupMonitoringSetting 并热更新 atomic config（无需重启容器）。
//
// 不能走"先 GET 整段再合并 PUT"：
//   1. 没有 GET /api/option/{key} 路由（只有 GET /api/option/ 列全量），单键 GET 必 404；
//   2. PUT 整段 key="group_monitoring_setting"（无 "."）不会被 handleConfigUpdate
//      识别为注册配置，只会写一条无效 option 行，atomic config 根本不更新。
// 只写这三个子键，其余字段（聚合周期 / 排除规则等）天然不受影响，无需合并。
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
  for (const u of updates) {
    const res = await api.put<ApiResponse<unknown>>('/api/option/', u)
    if (!res.data?.success) {
      throw new Error(res.data?.message || 'Failed to save monitoring settings')
    }
  }
}

export async function fetchGroupHistory(
  admin: boolean,
  groupName: string
): Promise<{ history: HistoryResponse['history']; intervalMinutes: number }> {
  const prefix = admin ? 'admin' : 'public'
  const res = await api.get<
    ApiResponse<HistoryResponse | HistoryResponse['history']>
  >(`/api/monitoring/${prefix}/groups/${encodeURIComponent(groupName)}/history`)
  if (!res.data.success) {
    return { history: [], intervalMinutes: 5 }
  }
  const raw = res.data.data
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
  const res = await api.get<ApiResponse<GroupDetail>>(
    `/api/monitoring/admin/groups/${encodeURIComponent(groupName)}`
  )
  if (!res.data.success) {
    throw new Error(res.data.message || 'Failed to fetch group detail')
  }
  return res.data.data
}

export async function refreshAdminMonitoring(): Promise<boolean> {
  const res = await api.post<ApiResponse<unknown>>(
    '/api/monitoring/admin/refresh'
  )
  return Boolean(res.data?.success)
}
