import { nanoid } from 'nanoid'
import { STORAGE_KEYS, DEFAULT_SESSION_TITLE } from '../constants'
import type {
  PlaygroundConfig,
  ParameterEnabled,
  Message,
  Session,
  SystemPreset,
} from '../types'
import { sanitizeMessagesOnLoad, getVersionText } from './message-utils'

/**
 * Load playground config from localStorage
 */
export function loadConfig(): Partial<PlaygroundConfig> {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.CONFIG)
    if (saved) {
      return JSON.parse(saved)
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load config:', error)
  }
  return {}
}

/**
 * Save playground config to localStorage
 */
export function saveConfig(config: Partial<PlaygroundConfig>): void {
  try {
    localStorage.setItem(STORAGE_KEYS.CONFIG, JSON.stringify(config))
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save config:', error)
  }
}

/**
 * Load parameter enabled state from localStorage
 */
export function loadParameterEnabled(): Partial<ParameterEnabled> {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.PARAMETER_ENABLED)
    if (saved) {
      return JSON.parse(saved)
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load parameter enabled:', error)
  }
  return {}
}

/**
 * Save parameter enabled state to localStorage
 */
export function saveParameterEnabled(
  parameterEnabled: Partial<ParameterEnabled>
): void {
  try {
    localStorage.setItem(
      STORAGE_KEYS.PARAMETER_ENABLED,
      JSON.stringify(parameterEnabled)
    )
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save parameter enabled:', error)
  }
}

/**
 * Load messages from localStorage
 */
function isValidStoredMessage(value: unknown): value is Message {
  if (!value || typeof value !== 'object') return false
  const m = value as Partial<Message>
  return (
    typeof m.key === 'string' &&
    typeof m.from === 'string' &&
    Array.isArray(m.versions)
  )
}

export function loadMessages(): Message[] | null {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.MESSAGES)
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      // Legacy / corrupted shapes: drop instead of crashing the page.
      if (!Array.isArray(parsed)) {
        localStorage.removeItem(STORAGE_KEYS.MESSAGES)
        return null
      }
      const filtered = parsed.filter(isValidStoredMessage) as Message[]
      const sanitized = sanitizeMessagesOnLoad(filtered)
      // Persist sanitized result to avoid re-sanitizing on subsequent loads
      if (sanitized !== filtered || filtered.length !== parsed.length) {
        saveMessages(sanitized)
      }
      return sanitized
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load messages:', error)
    // Bad JSON: clear the slot so the next load isn't doomed to repeat.
    try {
      localStorage.removeItem(STORAGE_KEYS.MESSAGES)
    } catch {
      /* empty */
    }
  }
  return null
}

/**
 * Save messages to localStorage
 */
export function saveMessages(messages: Message[]): void {
  try {
    localStorage.setItem(STORAGE_KEYS.MESSAGES, JSON.stringify(messages))
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save messages:', error)
  }
}

// ---------------------------------------------------------------------------
// Sessions（多会话）
// ---------------------------------------------------------------------------

/** 由首条用户消息派生标题（≤30 字），无则用默认标题。 */
export function deriveSessionTitle(messages: Message[]): string {
  const firstUser = messages.find((m) => m.from === 'user')
  const text = firstUser
    ? getVersionText(firstUser.versions?.[0]?.content ?? '').trim()
    : ''
  if (!text) return DEFAULT_SESSION_TITLE
  const oneLine = text.replace(/\s+/g, ' ')
  return oneLine.length > 30 ? oneLine.slice(0, 30) + '…' : oneLine
}

export function createSession(messages: Message[] = []): Session {
  const now = Date.now()
  return {
    id: nanoid(),
    title: deriveSessionTitle(messages),
    createdAt: now,
    updatedAt: now,
    messages,
  }
}

function isValidSession(v: unknown): v is Session {
  if (!v || typeof v !== 'object') return false
  const s = v as Partial<Session>
  return (
    typeof s.id === 'string' &&
    typeof s.title === 'string' &&
    Array.isArray(s.messages)
  )
}

/**
 * 加载全部会话 + 当前激活 id。
 * 迁移：无 SESSIONS 时，把旧的单一 MESSAGES 包成第一个会话（不丢历史）。
 * 始终保证至少返回一个会话。
 */
export function loadSessions(): { sessions: Session[]; activeId: string } {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.SESSIONS)
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      if (Array.isArray(parsed)) {
        const sessions = (parsed.filter(isValidSession) as Session[]).map(
          (s) => ({ ...s, messages: sanitizeMessagesOnLoad(s.messages) })
        )
        if (sessions.length > 0) {
          const savedActive =
            localStorage.getItem(STORAGE_KEYS.ACTIVE_SESSION) || ''
          const activeId = sessions.some((s) => s.id === savedActive)
            ? savedActive
            : sessions[0].id
          return { sessions, activeId }
        }
      }
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load sessions:', error)
  }
  // 迁移旧单会话 / 全新用户
  const legacy = loadMessages()
  const first = createSession(legacy && legacy.length ? legacy : [])
  const result = { sessions: [first], activeId: first.id }
  saveSessions(result.sessions, result.activeId)
  return result
}

export function saveSessions(sessions: Session[], activeId: string): void {
  try {
    localStorage.setItem(STORAGE_KEYS.SESSIONS, JSON.stringify(sessions))
    localStorage.setItem(STORAGE_KEYS.ACTIVE_SESSION, activeId)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save sessions:', error)
  }
}

// ---------------------------------------------------------------------------
// System prompt presets（跨会话共享）
// ---------------------------------------------------------------------------

export function loadSystemPresets(): SystemPreset[] {
  try {
    const saved = localStorage.getItem(STORAGE_KEYS.SYSTEM_PRESETS)
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      if (Array.isArray(parsed)) {
        return parsed.filter(
          (p): p is SystemPreset =>
            !!p &&
            typeof p === 'object' &&
            typeof (p as SystemPreset).id === 'string' &&
            typeof (p as SystemPreset).name === 'string' &&
            typeof (p as SystemPreset).content === 'string'
        )
      }
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load system presets:', error)
  }
  return []
}

export function saveSystemPresets(presets: SystemPreset[]): void {
  try {
    localStorage.setItem(
      STORAGE_KEYS.SYSTEM_PRESETS,
      JSON.stringify(presets)
    )
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save system presets:', error)
  }
}

/**
 * Clear all playground data
 */
export function clearPlaygroundData(): void {
  try {
    localStorage.removeItem(STORAGE_KEYS.CONFIG)
    localStorage.removeItem(STORAGE_KEYS.PARAMETER_ENABLED)
    localStorage.removeItem(STORAGE_KEYS.MESSAGES)
    localStorage.removeItem(STORAGE_KEYS.SESSIONS)
    localStorage.removeItem(STORAGE_KEYS.ACTIVE_SESSION)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to clear playground data:', error)
  }
}
