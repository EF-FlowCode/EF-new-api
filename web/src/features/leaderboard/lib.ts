// Compact token formatter — matches the leaderboard mock (1.65B / 407.0M / 30.3M).
export function formatTokens(n: number): string {
  if (!Number.isFinite(n)) return '0'
  const abs = Math.abs(n)
  if (abs >= 1e9) return (n / 1e9).toFixed(2) + 'B'
  if (abs >= 1e6) return (n / 1e6).toFixed(1) + 'M'
  if (abs >= 1e3) return (n / 1e3).toFixed(1) + 'K'
  return String(Math.round(n))
}

// Bucket-start unix seconds -> axis label. Day buckets render "May 27" (en, to
// match the mock); hour buckets render "13:00".
export function bucketLabel(
  timeSeconds: number,
  bucket: 'hour' | 'day'
): string {
  const d = new Date(timeSeconds * 1000)
  if (bucket === 'hour') {
    return `${String(d.getHours()).padStart(2, '0')}:00`
  }
  return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

// eflow: 把 unix 秒按东八区(UTC+8)墙钟格式化。平台「今天」按北京时间切桶，
// 这里统一用东八区渲染区间起止，避免随浏览器时区漂移。withTime 时带 HH:mm。
export function fmtBeijing(unix: number, withTime = false): string {
  const d = new Date((unix + 8 * 3600) * 1000) // 平移后读 UTC 字段 = 东八区墙钟
  const mo = String(d.getUTCMonth() + 1).padStart(2, '0')
  const da = String(d.getUTCDate()).padStart(2, '0')
  if (!withTime) return `${mo}-${da}`
  const h = String(d.getUTCHours()).padStart(2, '0')
  const mi = String(d.getUTCMinutes()).padStart(2, '0')
  return `${mo}-${da} ${h}:${mi}`
}

// Trend percentage -> short signed label, e.g. "↑159%" / "↓33.0%".
export function formatTrend(pct: number): { label: string; up: boolean } {
  const up = pct >= 0
  const abs = Math.abs(pct)
  // eflow: 涨幅极大时（短周期 / 小基数常见，原始数据会出现 ↑28722% 这种天文数字）
  // 改用「倍数」表达，更易读也更诚实：cur/prev = 1 + pct/100，例 28722% → ↑288×。
  // 下跌恒在 0~100% 区间，仍用百分比。
  if (up && abs >= 1000) {
    const x = Math.round(1 + abs / 100)
    return { label: `↑${x}×`, up }
  }
  // Large ratios as integers, small ones keep one decimal (mirrors the mock).
  const num = abs >= 100 ? Math.round(abs).toString() : abs.toFixed(1)
  return { label: `${up ? '↑' : '↓'}${num}%`, up }
}
