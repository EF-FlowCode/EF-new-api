export type AdvancedCustomAuthType = 'none' | 'header' | 'query'
export type AdvancedCustomConverter =
  | 'none'
  | 'anthropic_messages_to_openai_chat_completions'
  | 'openai_chat_completions_to_anthropic_messages'
  | 'openai_chat_completions_to_openai_responses'
  | 'openai_responses_to_openai_chat_completions'
  | 'openai_responses_to_gemini_generate_content'
  | 'gemini_generate_content_to_openai_chat_completions'
  | 'openai_chat_completions_to_gemini_generate_content'

export type AdvancedCustomRouteAuth = {
  type?: AdvancedCustomAuthType | string
  name?: string
  value?: string
}

export type AdvancedCustomRoute = {
  incoming_path: string
  upstream_path: string
  converter?: AdvancedCustomConverter | string
  models?: string[]
  auth?: AdvancedCustomRouteAuth
  /** Forward the client body unchanged; only for native-forwarding routes. */
  pass_through_body_enabled?: boolean
}

export type AdvancedCustomConfig = {
  advanced_routes: AdvancedCustomRoute[]
}

export type AdvancedCustomEditorState = {
  jsonText: string
  modelListPath: string
  balancePath: string
}

export const ADVANCED_CUSTOM_MODEL_LIST_PATH = '/v1/models'
export const ADVANCED_CUSTOM_BALANCE_PATH =
  '/v1/dashboard/billing/credit_grants'

export const ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS = [
  '/v1/chat/completions',
  '/v1/responses',
  '/v1/responses/compact',
  '/v1/alpha/search',
  '/v1/embeddings',
  '/v1/images/generations',
  '/v1/rerank',
  '/v1/messages',
  '/v1beta/models/{model}:generateContent',
  '/v1beta/models/{model}:streamGenerateContent',
]

export const ADVANCED_CUSTOM_CONVERTER_OPTIONS: Array<{
  value: AdvancedCustomConverter
  label: string
}> = [
  { value: 'none', label: 'Native forwarding' },
  {
    value: 'anthropic_messages_to_openai_chat_completions',
    label: 'Anthropic Messages to OpenAI Chat',
  },
  {
    value: 'openai_chat_completions_to_anthropic_messages',
    label: 'OpenAI Chat to Anthropic Messages',
  },
  {
    value: 'openai_chat_completions_to_openai_responses',
    label: 'OpenAI Chat to OpenAI Responses',
  },
  {
    value: 'openai_responses_to_openai_chat_completions',
    label: 'OpenAI Responses to OpenAI Chat',
  },
  {
    value: 'openai_responses_to_gemini_generate_content',
    label: 'OpenAI Responses to Gemini',
  },
  {
    value: 'gemini_generate_content_to_openai_chat_completions',
    label: 'Gemini to OpenAI Chat',
  },
  {
    value: 'openai_chat_completions_to_gemini_generate_content',
    label: 'OpenAI Chat to Gemini',
  },
]

export function isAdvancedCustomManagementPath(path: string): boolean {
  const trimmed = path.trim()
  return (
    trimmed === ADVANCED_CUSTOM_MODEL_LIST_PATH ||
    trimmed === ADVANCED_CUSTOM_BALANCE_PATH
  )
}

const ADVANCED_CUSTOM_GEMINI_UPSTREAM_PATH =
  '/v1beta/models/{model}:generateContent'

/**
 * Upstream path a converter's target protocol normally uses. The visual editor
 * uses it to prefill upstream_path; streaming Gemini requests are switched to
 * :streamGenerateContent by the relay adaptor.
 */
export function getAdvancedCustomConverterUpstreamPath(
  converter: string,
  incomingPath: string
): string {
  switch (converter) {
    case 'anthropic_messages_to_openai_chat_completions':
    case 'openai_responses_to_openai_chat_completions':
    case 'gemini_generate_content_to_openai_chat_completions':
      return '/v1/chat/completions'
    case 'openai_chat_completions_to_anthropic_messages':
      return '/v1/messages'
    case 'openai_chat_completions_to_openai_responses':
      return '/v1/responses'
    case 'openai_responses_to_gemini_generate_content':
    case 'openai_chat_completions_to_gemini_generate_content':
      return ADVANCED_CUSTOM_GEMINI_UPSTREAM_PATH
    default:
      return incomingPath
  }
}

/**
 * Mirror of relaykit advancedCustomEndpointTypeFromIncomingPath: the inbound
 * endpoint type a relay route serves, or null for paths without one.
 */
export function getAdvancedCustomEndpointType(
  incomingPath: string
): string | null {
  const path = incomingPath.trim()
  switch (path) {
    case '/v1/chat/completions':
      return 'openai'
    case '/v1/responses':
      return 'openai-response'
    case '/v1/responses/compact':
      return 'openai-response-compact'
    case '/v1/alpha/search':
      return 'openai-alpha-search'
    case '/v1/messages':
      return 'anthropic'
    case '/v1/rerank':
      return 'jina-rerank'
    case '/v1/images/generations':
      return 'image-generation'
    case '/v1/embeddings':
      return 'embeddings'
    default:
      if (
        path.startsWith('/v1beta/models/') &&
        (path.includes(':generateContent') ||
          path.includes(':streamGenerateContent'))
      )
        return 'gemini'
      return null
  }
}

/** Relay (non-management) routes of a raw Advanced Custom value. */
export function getAdvancedCustomRelayRoutes(
  value: string | undefined
): AdvancedCustomRoute[] {
  const config = parseAdvancedCustomConfig(value || '')
  if (!config) return []
  return config.advanced_routes.filter(
    (route) => !isAdvancedCustomManagementPath(route.incoming_path)
  )
}

export function createAdvancedCustomConfig(): AdvancedCustomConfig {
  return { advanced_routes: [] }
}

export function parseAdvancedCustomConfig(
  value: string
): AdvancedCustomConfig | null {
  if (!value.trim()) return createAdvancedCustomConfig()
  try {
    const parsed: unknown = JSON.parse(value)
    if (!isAdvancedCustomConfigShape(parsed)) return null
    return normalizeAdvancedCustomConfig(parsed as AdvancedCustomConfig)
  } catch {
    return null
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isOptionalString(value: unknown): boolean {
  return value === undefined || value === null || typeof value === 'string'
}

function isAdvancedCustomConfigShape(value: unknown): boolean {
  if (!isRecord(value) || !Array.isArray(value.advanced_routes)) return false
  return value.advanced_routes.every((route) => {
    if (!isRecord(route)) return false
    if (
      !isOptionalString(route.incoming_path) ||
      !isOptionalString(route.upstream_path) ||
      !isOptionalString(route.converter)
    ) {
      return false
    }
    if (
      route.models !== undefined &&
      route.models !== null &&
      (!Array.isArray(route.models) ||
        route.models.some((model) => !isOptionalString(model)))
    ) {
      return false
    }
    if (
      route.pass_through_body_enabled !== undefined &&
      route.pass_through_body_enabled !== null &&
      typeof route.pass_through_body_enabled !== 'boolean'
    )
      return false
    if (route.auth === undefined || route.auth === null) return true
    if (!isRecord(route.auth)) return false
    return (
      isOptionalString(route.auth.type) &&
      isOptionalString(route.auth.name) &&
      isOptionalString(route.auth.value)
    )
  })
}

export function normalizeAdvancedCustomConfig(
  config: AdvancedCustomConfig
): AdvancedCustomConfig {
  const routes = Array.isArray(config?.advanced_routes)
    ? config.advanced_routes
    : []
  return {
    advanced_routes: routes.map((route) => {
      const rawRoute = route as AdvancedCustomRoute
      const normalized: AdvancedCustomRoute = {
        incoming_path:
          typeof rawRoute?.incoming_path === 'string'
            ? rawRoute.incoming_path.trim()
            : '',
        upstream_path:
          typeof rawRoute?.upstream_path === 'string'
            ? rawRoute.upstream_path.trim()
            : '',
        converter:
          typeof rawRoute?.converter === 'string' && rawRoute.converter.trim()
            ? rawRoute.converter.trim()
            : 'none',
        models: Array.isArray(rawRoute?.models)
          ? rawRoute.models
              .filter((model): model is string => typeof model === 'string')
              .map((model) => model.trim())
              .filter(Boolean)
          : [],
      }
      if (rawRoute?.pass_through_body_enabled === true)
        normalized.pass_through_body_enabled = true
      if (rawRoute?.auth && typeof rawRoute.auth === 'object') {
        normalized.auth = {
          type:
            typeof rawRoute.auth.type === 'string'
              ? rawRoute.auth.type.trim()
              : '',
          name:
            typeof rawRoute.auth.name === 'string' ? rawRoute.auth.name : '',
          value:
            typeof rawRoute.auth.value === 'string' ? rawRoute.auth.value : '',
        }
      }
      return normalized
    }),
  }
}

export function stringifyAdvancedCustomConfig(
  config: AdvancedCustomConfig
): string {
  return JSON.stringify(normalizeAdvancedCustomConfig(config), null, 2)
}

export function validateAdvancedCustomConfig(
  config: AdvancedCustomConfig
): string | null {
  const routes = config?.advanced_routes
  if (!Array.isArray(routes)) return 'advanced_routes must be an array'
  if (routes.length === 0) return 'advanced_custom requires at least one route'

  const seenManagement = new Set<string>()
  const seenByPath = new Map<
    string,
    { catchAllIndex: number | null; models: Set<string> }
  >()
  for (const [index, route] of routes.entries()) {
    if (!route || typeof route !== 'object')
      return `Route ${index + 1}: route must be an object`
    const incoming =
      typeof route.incoming_path === 'string' ? route.incoming_path.trim() : ''
    const upstream =
      typeof route.upstream_path === 'string' ? route.upstream_path.trim() : ''
    const converter =
      typeof route.converter === 'string' && route.converter.trim()
        ? route.converter.trim()
        : 'none'
    if (!incoming) return `Route ${index + 1}: incoming_path is required`
    if (!incoming.startsWith('/'))
      return `Route ${index + 1}: incoming_path must start with /`
    if (incoming.includes('?'))
      return `Route ${index + 1}: incoming_path must not include query`
    if (!upstream || !isValidAdvancedCustomUpstreamTarget(upstream)) {
      return `Route ${index + 1}: upstream_path must be a path or full URL`
    }

    if (route.models !== undefined && !Array.isArray(route.models))
      return `Route ${index + 1}: models must be an array`
    const models = Array.isArray(route.models)
      ? route.models
          .filter((model): model is string => typeof model === 'string')
          .map((model) => model.trim())
          .filter(Boolean)
      : []

    if (
      incoming === ADVANCED_CUSTOM_MODEL_LIST_PATH ||
      incoming === ADVANCED_CUSTOM_BALANCE_PATH
    ) {
      if (seenManagement.has(incoming))
        return `Route ${index + 1}: duplicate management route ${incoming}`
      seenManagement.add(incoming)
      if (models.length > 0)
        return `Route ${index + 1}: management route models must be empty`
      if (converter !== 'none')
        return `Route ${index + 1}: management route converter must be none`
      if (upstream.includes('{model}'))
        return `Route ${index + 1}: management route cannot use {model}`
      if (route.pass_through_body_enabled)
        return `Route ${index + 1}: management route cannot pass through the body`
    }

    const authError = validateAdvancedCustomRouteAuth(route.auth)
    if (authError) return `Route ${index + 1}: ${authError}`

    if (
      incoming === ADVANCED_CUSTOM_MODEL_LIST_PATH ||
      incoming === ADVANCED_CUSTOM_BALANCE_PATH
    )
      continue

    let pathState = seenByPath.get(incoming)
    if (!pathState) {
      pathState = { catchAllIndex: null, models: new Set<string>() }
      seenByPath.set(incoming, pathState)
    }
    if (models.length === 0) {
      if (pathState.catchAllIndex !== null)
        return `Route ${index + 1}: duplicate catch-all route`
      pathState.catchAllIndex = index
    } else {
      if (pathState.catchAllIndex !== null)
        return `Route ${index + 1}: catch-all route must be last`
      const modelsInRoute = new Set<string>()
      for (const model of models) {
        if (
          model.startsWith('re:') &&
          !isValidAdvancedCustomModelRegex(model.slice(3))
        )
          return `Route ${index + 1}: invalid model regex ${model}`
        if (modelsInRoute.has(model) || pathState.models.has(model))
          return `Route ${index + 1}: duplicate model ${model}`
        modelsInRoute.add(model)
        pathState.models.add(model)
      }
    }

    if (
      !ADVANCED_CUSTOM_CONVERTER_OPTIONS.some(
        (option) => option.value === converter
      )
    )
      return `Route ${index + 1}: converter is not registered`
    if (
      !isAdvancedCustomConverterPathAllowed(
        incoming,
        converter as AdvancedCustomConverter
      )
    )
      return `Route ${index + 1}: converter does not match incoming_path`
    if (route.pass_through_body_enabled && converter !== 'none')
      return `Route ${index + 1}: body pass-through requires native forwarding`
  }
  return null
}

function isValidAdvancedCustomUpstreamTarget(value: string): boolean {
  if (value.startsWith('/')) return !value.startsWith('//')
  try {
    const parsed = new URL(value)
    return (
      Boolean(parsed.host) &&
      (parsed.protocol === 'http:' || parsed.protocol === 'https:')
    )
  } catch {
    return false
  }
}

function isValidAdvancedCustomModelRegex(pattern: string): boolean {
  if (!pattern) return false
  const inlineFlags = pattern.match(/^\(\?([imsU]+)\)/)
  const source = inlineFlags ? pattern.slice(inlineFlags[0].length) : pattern
  const flags = inlineFlags ? inlineFlags[1].replace('U', '') : ''
  try {
    new RegExp(source, flags)
    return true
  } catch {
    // Go's RE2 accepts a few constructs that JavaScript does not. Keep those
    // patterns for server-side validation while still rejecting unbalanced
    // delimiters, which catches the common malformed cases locally.
    return hasBalancedAdvancedCustomRegex(pattern)
  }
}

function hasBalancedAdvancedCustomRegex(pattern: string): boolean {
  let escaped = false
  let inClass = false
  let depth = 0
  for (const char of pattern) {
    if (escaped) {
      escaped = false
      continue
    }
    if (char === '\\') {
      escaped = true
      continue
    }
    if (char === '[' && !inClass) {
      inClass = true
      continue
    }
    if (char === ']' && inClass) {
      inClass = false
      continue
    }
    if (!inClass && char === '(') depth += 1
    if (!inClass && char === ')' && --depth < 0) return false
  }
  return !escaped && !inClass && depth === 0
}

export function isAdvancedCustomConverterPathAllowed(
  incoming: string,
  converter: AdvancedCustomConverter
): boolean {
  if (converter === 'none') return true
  if (incoming === '/v1/alpha/search') return false
  switch (converter) {
    case 'anthropic_messages_to_openai_chat_completions':
      return incoming === '/v1/messages'
    case 'openai_chat_completions_to_anthropic_messages':
    case 'openai_chat_completions_to_openai_responses':
    case 'openai_chat_completions_to_gemini_generate_content':
      return incoming === '/v1/chat/completions'
    case 'openai_responses_to_openai_chat_completions':
    case 'openai_responses_to_gemini_generate_content':
      return incoming === '/v1/responses'
    case 'gemini_generate_content_to_openai_chat_completions':
      return (
        incoming.includes(':generateContent') ||
        incoming.includes(':streamGenerateContent')
      )
    default:
      return false
  }
}

function validateAdvancedCustomRouteAuth(
  auth: AdvancedCustomRoute['auth']
): string | null {
  if (auth === undefined || auth === null) return null
  if (typeof auth !== 'object') return 'auth must be an object'
  const type = typeof auth.type === 'string' ? auth.type.trim() : ''
  if (type === 'none') return null
  if (type !== 'header' && type !== 'query') return 'invalid auth type'
  if (typeof auth.name !== 'string' || !auth.name.trim())
    return 'auth.name is required'
  if (typeof auth.value !== 'string' || !auth.value.trim())
    return 'auth.value is required'
  return null
}

export function getAdvancedCustomManagementRoute(
  config: AdvancedCustomConfig,
  path: string
): AdvancedCustomRoute | undefined {
  return config.advanced_routes.find(
    (route) => route.incoming_path.trim() === path
  )
}

export function createAdvancedCustomEditorState(
  value: string
): AdvancedCustomEditorState {
  const config =
    parseAdvancedCustomConfig(value) || createAdvancedCustomConfig()
  const normalized = normalizeAdvancedCustomConfig(config)
  return {
    jsonText: stringifyAdvancedCustomConfig(normalized),
    modelListPath:
      getAdvancedCustomManagementRoute(
        normalized,
        ADVANCED_CUSTOM_MODEL_LIST_PATH
      )?.upstream_path || '',
    balancePath:
      getAdvancedCustomManagementRoute(normalized, ADVANCED_CUSTOM_BALANCE_PATH)
        ?.upstream_path || '',
  }
}

/**
 * Return whether a raw Advanced Custom value (or a channel settings JSON
 * object containing one) declares the exact model discovery route.  The
 * helper deliberately does not infer support from a generic Custom channel;
 * legacy Custom channels must keep their existing UI behavior.
 */
export function hasAdvancedCustomModelListRoute(value: unknown): boolean {
  if (!value) return false
  let candidate: unknown = value
  if (typeof value === 'string') {
    try {
      candidate = JSON.parse(value) as unknown
    } catch {
      return false
    }
  }
  if (!isRecord(candidate)) return false
  const config = isRecord(candidate.advanced_custom)
    ? candidate.advanced_custom
    : candidate
  if (!Array.isArray(config.advanced_routes)) return false
  return config.advanced_routes.some(
    (route) =>
      isRecord(route) &&
      typeof route.incoming_path === 'string' &&
      route.incoming_path.trim() === ADVANCED_CUSTOM_MODEL_LIST_PATH
  )
}
