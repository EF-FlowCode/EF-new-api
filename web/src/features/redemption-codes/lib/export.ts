// eflow: 按上游 524455fac —— 新建兑换码后可选把本次生成的码存成 TXT / Markdown 文件。
// 纯函数，便于单测；文件下载在 redemptions-export-dialog.tsx。

export type RedemptionExportData = {
  keys: string[]
  name: string
  quota: string
}

export type RedemptionExportFormat = 'txt' | 'md'

export type RedemptionExportOptions = {
  format: RedemptionExportFormat
  includeName: boolean
  includeQuota: boolean
  /** 列标题（已翻译），只用于 Markdown 表头。 */
  headers: { name: string; code: string; quota: string }
}

/** 单元格内的制表符、换行会破坏 TXT 列与 Markdown 行，统一压成一个空格。 */
function flattenCell(value: string): string {
  return value.replaceAll(/[\t\r\n]+/g, ' ')
}

function escapeMarkdownCell(value: string): string {
  return value.replaceAll('&', '&amp;').replaceAll(/[\\`*_[\]|<>]/g, '\\$&')
}

export function buildRedemptionExportContent(
  data: RedemptionExportData,
  options: RedemptionExportOptions
): string {
  const headers: string[] = []
  if (options.includeName) headers.push(options.headers.name)
  headers.push(options.headers.code)
  if (options.includeQuota) headers.push(options.headers.quota)

  const rows = data.keys.map((key) => {
    const row: string[] = []
    if (options.includeName) row.push(data.name)
    row.push(key)
    if (options.includeQuota) row.push(data.quota)
    return row.map(flattenCell)
  })

  if (options.format === 'txt') {
    return `${rows.map((row) => row.join('\t')).join('\n')}\n`
  }

  const lines = [
    headers.map(flattenCell).map(escapeMarkdownCell),
    headers.map(() => '---'),
    ...rows.map((row) => row.map(escapeMarkdownCell)),
  ].map((cells) => `| ${cells.join(' | ')} |`)
  return `${lines.join('\n')}\n`
}

export function redemptionExportMimeType(format: RedemptionExportFormat) {
  return format === 'md'
    ? 'text/markdown;charset=utf-8'
    : 'text/plain;charset=utf-8'
}
