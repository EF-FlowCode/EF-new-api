/**
 * 按界面语言格式化相对时间（如「2 小时前」/「2 hours ago」）。
 * dayjs 的 fromNow 未加载任何 locale，始终输出英文；这里改用浏览器内置的
 * Intl.RelativeTimeFormat，跟随 i18n 当前语言。
 */

/** i18n 语言码 → Intl locale；无法识别时交给浏览器默认语言。 */
export function toIntlLocale(language?: string | null): string | undefined {
  if (!language) return undefined
  try {
    return Intl.getCanonicalLocales(language)[0]
  } catch {
    return undefined
  }
}

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ['year', 31536000],
  ['month', 2592000],
  ['day', 86400],
  ['hour', 3600],
  ['minute', 60],
]

/**
 * @param timestampMs 目标时间（毫秒）；无效值返回 '-'
 * @param locale Intl locale，例如 toIntlLocale(i18n.resolvedLanguage)
 * @param now 当前时间（毫秒），便于测试
 */
export function formatRelativeTimestamp(
  timestampMs: number,
  locale?: string,
  now: number = Date.now()
): string {
  if (!Number.isFinite(timestampMs) || timestampMs <= 0) return '-'
  const diffSeconds = Math.round((timestampMs - now) / 1000)
  const abs = Math.abs(diffSeconds)
  const formatter = new Intl.RelativeTimeFormat(locale, { numeric: 'always' })
  for (const [unit, seconds] of UNITS) {
    if (abs >= seconds) {
      return formatter.format(Math.round(diffSeconds / seconds), unit)
    }
  }
  return formatter.format(diffSeconds, 'second')
}
