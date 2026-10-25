const GROUP_PREFIXES = [
  ['Tools_', 'Tools'],
  ['vip_', 'VIP'],
  ['free_', 'Free'],
  ['svip_', 'SVIP'],
  ['work_', 'Work'],
] as const

const GROUP_LAYOUT_PREFIX = /^(\d+)_/

const GROUP_TOKEN_LABELS: Record<string, string> = {
  api: 'API',
  api2: 'API 2',
  asr: 'ASR',
  aws: 'AWS',
  codex: 'Codex',
  gemini: 'Gemini',
  gpt: 'GPT',
  image: 'Image',
  knowledge: 'Knowledge',
  max: 'MAX',
  meta: 'Meta',
  rpm: 'RPM',
  tts: 'TTS',
  vip: 'VIP',
  wos: 'WOS',
}

function formatGroupToken(token: string): string {
  const normalized = token.trim()
  if (!normalized) return ''

  const knownLabel = GROUP_TOKEN_LABELS[normalized.toLowerCase()]
  if (knownLabel) return knownLabel
  if (/^[A-Z0-9]+$/.test(normalized)) return normalized

  return normalized.charAt(0).toUpperCase() + normalized.slice(1).toLowerCase()
}

export function formatGroupLabel(group: string): string {
  const trimmed = group.trim()
  if (!trimmed) return trimmed

  const displayKey = trimmed.replace(GROUP_LAYOUT_PREFIX, '')
  const prefix = GROUP_PREFIXES.find(([value]) => displayKey.startsWith(value))
  if (prefix) {
    const suffix = displayKey
      .slice(prefix[0].length)
      .split('_')
      .map(formatGroupToken)
      .filter(Boolean)
      .join(' ')
    return suffix ? `${prefix[1]} · ${suffix}` : prefix[1]
  }

  return displayKey.split('_').map(formatGroupToken).filter(Boolean).join(' ')
}

export function getGroupLayoutRow(group: string): number | undefined {
  const match = group.trim().match(GROUP_LAYOUT_PREFIX)
  if (!match) return undefined

  const row = Number(match[1])
  return Number.isSafeInteger(row) ? row : undefined
}

export function formatGroupRatio(
  ratio: number | undefined
): string | undefined {
  if (ratio == null || !Number.isFinite(ratio)) return undefined
  const formatted = Number.isInteger(ratio)
    ? ratio.toString()
    : ratio.toFixed(4).replace(/0+$/, '').replace(/\.$/, '')
  return `${formatted}x`
}
