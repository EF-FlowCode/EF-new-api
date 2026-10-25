import { nanoid } from 'nanoid'
import { useAuthStore } from '@/stores/auth-store'
import { STORAGE_KEYS, DEFAULT_SESSION_TITLE } from '../constants'
import type {
  PlaygroundConfig,
  ParameterEnabled,
  Message,
  Session,
  SystemPreset,
} from '../types'
import { sanitizeMessagesOnLoad, getVersionText } from './message-utils'

// ---------------------------------------------------------------------------
// 按账号隔离：同一浏览器切换账号后，下一个用户不能看到上一个用户的对话、
// 配置与提示词预设。所有 Playground 数据都写到 `<key>:<userId>` 下。
// ---------------------------------------------------------------------------

type StorageKey = (typeof STORAGE_KEYS)[keyof typeof STORAGE_KEYS]

/**
 * 旧版本写在固定键下的数据没有归属，不能分给下一个登录的账号，直接删除
 * （已有的 Playground 历史因此会被清空一次）。
 */
function removeLegacySharedData(): void {
  for (const legacyKey of Object.values(STORAGE_KEYS)) {
    localStorage.removeItem(legacyKey)
  }
}

/** 当前登录账号的存储键；未登录返回 null，调用方既不读也不写。 */
function userStorageKey(baseKey: StorageKey): string | null {
  removeLegacySharedData()
  const userId = useAuthStore.getState().auth.user?.id
  return userId == null ? null : `${baseKey}:${userId}`
}

function readItem(baseKey: StorageKey): string | null {
  const key = userStorageKey(baseKey)
  return key ? localStorage.getItem(key) : null
}

function writeItem(baseKey: StorageKey, value: string): void {
  const key = userStorageKey(baseKey)
  if (key) localStorage.setItem(key, value)
}

function removeItem(baseKey: StorageKey): void {
  const key = userStorageKey(baseKey)
  if (key) localStorage.removeItem(key)
}

/**
 * Load playground config from localStorage
 */
export function loadConfig(): Partial<PlaygroundConfig> {
  try {
    const saved = readItem(STORAGE_KEYS.CONFIG)
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
    writeItem(STORAGE_KEYS.CONFIG, JSON.stringify(config))
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
    const saved = readItem(STORAGE_KEYS.PARAMETER_ENABLED)
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
    writeItem(STORAGE_KEYS.PARAMETER_ENABLED, JSON.stringify(parameterEnabled))
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
    const saved = readItem(STORAGE_KEYS.MESSAGES)
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      // Legacy / corrupted shapes: drop instead of crashing the page.
      if (!Array.isArray(parsed)) {
        removeItem(STORAGE_KEYS.MESSAGES)
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
      removeItem(STORAGE_KEYS.MESSAGES)
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
    writeItem(STORAGE_KEYS.MESSAGES, JSON.stringify(messages))
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
    const saved = readItem(STORAGE_KEYS.SESSIONS)
    if (saved) {
      const parsed: unknown = JSON.parse(saved)
      if (Array.isArray(parsed)) {
        const sessions = (parsed.filter(isValidSession) as Session[]).map(
          (s) => ({
            ...s,
            messages: sanitizeMessagesOnLoad(
              s.messages.filter(isValidStoredMessage)
            ),
          })
        )
        if (sessions.length > 0) {
          const savedActive = readItem(STORAGE_KEYS.ACTIVE_SESSION) || ''
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
    // 解析失败时不回写：保留原始存储，避免用一个空会话覆盖全部历史。
    const fallback = createSession([])
    return { sessions: [fallback], activeId: fallback.id }
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
    writeItem(STORAGE_KEYS.SESSIONS, JSON.stringify(sessions))
    writeItem(STORAGE_KEYS.ACTIVE_SESSION, activeId)
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
    const saved = readItem(STORAGE_KEYS.SYSTEM_PRESETS)
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
    writeItem(STORAGE_KEYS.SYSTEM_PRESETS, JSON.stringify(presets))
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save system presets:', error)
  }
}

/**
 * Clear the signed-in user's playground data
 */
export function clearPlaygroundData(): void {
  try {
    removeItem(STORAGE_KEYS.CONFIG)
    removeItem(STORAGE_KEYS.PARAMETER_ENABLED)
    removeItem(STORAGE_KEYS.MESSAGES)
    removeItem(STORAGE_KEYS.SESSIONS)
    removeItem(STORAGE_KEYS.ACTIVE_SESSION)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to clear playground data:', error)
  }
}
