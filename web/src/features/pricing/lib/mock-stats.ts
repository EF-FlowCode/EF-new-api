/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

// eflow: 精简版——上游原 mock-stats 含一堆基于 ./seed 的假数据生成器
// (buildGroupPerformance / buildUptimeSeries / buildAppRankings 等),
// eflow 不展示假数据,模型详情性能页全部用真实 GetPerfMetrics 数据。
// 这里只保留性能 tab 实际用到的两个数据形状 + 纯聚合函数,不引入 ./seed。

export type LatencyTimePoint = {
  timestamp: string
  group: string
  ttft_ms: number
}

export type UptimeDayPoint = {
  date: string
  uptime_pct: number
  incidents: number
  outage_minutes: number
}

export function aggregateUptime(points: UptimeDayPoint[]): {
  uptime_pct: number
  incidents: number
  outage_minutes: number
} {
  if (points.length === 0) {
    return { uptime_pct: 0, incidents: 0, outage_minutes: 0 }
  }
  const incidents = points.reduce((s, p) => s + p.incidents, 0)
  const outageMinutes = points.reduce((s, p) => s + p.outage_minutes, 0)
  const totalMinutes = points.length * 1_440
  const uptimePct = ((totalMinutes - outageMinutes) / totalMinutes) * 100
  return {
    incidents,
    outage_minutes: outageMinutes,
    uptime_pct: Math.round(uptimePct * 1000) / 1000,
  }
}
