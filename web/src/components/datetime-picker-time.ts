/** 日期时间选择器的时分秒处理：精确到秒，空值不产生非法日期。 */

export const DEFAULT_TIME = '00:00:00'

const pad = (n: number) => n.toString().padStart(2, '0')

/** Date → `HH:mm:ss`（本地时区），供 <input type="time" step="1"> 显示。 */
export function formatTimeOfDay(date: Date): string {
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`
}

/**
 * 把 `HH:mm[:ss]` 写到 date 的时分秒上，返回新的 Date。
 * - 浏览器在秒为 0 时可能省略 `:00`，缺省秒按 0 处理；
 * - 输入框被清空或只清了一段（Backspace）时浏览器给出 ''，或解析出非法数值，
 *   返回 null，调用方保留当前时间，不向外发出 Invalid Date。
 */
const TIME_PATTERN = /^(\d{1,2}):(\d{2})(?::(\d{2}))?$/

export function applyTimeOfDay(date: Date, time: string): Date | null {
  const match = TIME_PATTERN.exec(time)
  if (!match) return null
  const hours = Number(match[1])
  const minutes = Number(match[2])
  const seconds = match[3] === undefined ? 0 : Number(match[3])
  if (hours > 23 || minutes > 59 || seconds > 59) return null
  const next = new Date(date)
  next.setHours(hours, minutes, seconds, 0)
  return next
}
