export type PricingEndpointRow = {
  group: string
  type: string
  path: string
  method: string
}

export type ModelContextRow = {
  model: string
  tokens: number
}

export type EndpointOverrideConfig = {
  fallback: PricingEndpointRow
  rows: PricingEndpointRow[]
}

export type ModelContextConfig = {
  fallbackTokens: number | null
  rows: ModelContextRow[]
}

export const BUILT_IN_ENDPOINT_FALLBACK: PricingEndpointRow = {
  group: 'default',
  type: 'openai',
  path: '/v1/chat/completions',
  method: 'POST',
}

type JsonRecord = Record<string, unknown>

export function isObjectJson(value: string): boolean {
  try {
    const parsed: unknown = JSON.parse(value || '{}')
    return Boolean(
      parsed && typeof parsed === 'object' && !Array.isArray(parsed)
    )
  } catch {
    return false
  }
}

function parseRecord(value: string): JsonRecord {
  try {
    const parsed: unknown = JSON.parse(value || '{}')
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      return {}
    }
    return parsed as JsonRecord
  } catch {
    return {}
  }
}

export function parseEndpointOverrideRows(value: string): PricingEndpointRow[] {
  return Object.entries(parseRecord(value)).flatMap(([group, raw]) => {
    if (typeof raw === 'string') {
      return [
        {
          group,
          type: 'openai',
          path: raw,
          method: 'POST',
        },
      ]
    }

    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return []
    const config = raw as JsonRecord
    return [
      {
        group,
        type: typeof config.type === 'string' ? config.type : 'openai',
        path: typeof config.path === 'string' ? config.path : '',
        method:
          typeof config.method === 'string'
            ? config.method.toUpperCase()
            : 'POST',
      },
    ]
  })
}

export function serializeEndpointOverrideRows(
  rows: PricingEndpointRow[]
): string {
  const result: JsonRecord = {}
  for (const row of rows) {
    const group = row.group.trim()
    const path = row.path.trim()
    if (!group && !path) continue

    const config: JsonRecord = {
      type: row.type.trim() || 'openai',
      path,
      method: row.method.trim().toUpperCase() || 'POST',
    }
    result[group] = config
  }
  return JSON.stringify(result, null, 2)
}

export function parseEndpointOverrideConfig(
  value: string
): EndpointOverrideConfig {
  const parsedRows = parseEndpointOverrideRows(value)
  const configuredFallback = parsedRows.find((row) => row.group === 'default')

  return {
    fallback: configuredFallback ?? { ...BUILT_IN_ENDPOINT_FALLBACK },
    rows: parsedRows.filter((row) => row.group !== 'default'),
  }
}

export function serializeEndpointOverrideConfig(
  fallback: PricingEndpointRow,
  rows: PricingEndpointRow[]
): string {
  return serializeEndpointOverrideRows([
    { ...fallback, group: 'default' },
    ...rows.filter((row) => row.group.trim() !== 'default'),
  ])
}

export function parseModelContextRows(value: string): ModelContextRow[] {
  return Object.entries(parseRecord(value)).flatMap(([model, raw]) => {
    if (typeof raw !== 'number') return []
    return [{ model, tokens: raw }]
  })
}

export function serializeModelContextRows(rows: ModelContextRow[]): string {
  const result: Record<string, number> = {}
  for (const row of rows) {
    const model = row.model.trim()
    if (!model && !row.tokens) continue
    result[model] = Number.isFinite(row.tokens) ? row.tokens : 0
  }
  return JSON.stringify(result, null, 2)
}

export function parseModelContextConfig(value: string): ModelContextConfig {
  const parsedRows = parseModelContextRows(value)
  const configuredFallback = parsedRows.find((row) => row.model === 'default')

  return {
    fallbackTokens: configuredFallback?.tokens ?? null,
    rows: parsedRows.filter((row) => row.model !== 'default'),
  }
}

export function serializeModelContextConfig(
  fallbackTokens: number | null,
  rows: ModelContextRow[]
): string {
  const specificRows = rows.filter((row) => row.model.trim() !== 'default')
  if (!fallbackTokens || fallbackTokens <= 0) {
    return serializeModelContextRows(specificRows)
  }

  return serializeModelContextRows([
    { model: 'default', tokens: fallbackTokens },
    ...specificRows,
  ])
}
