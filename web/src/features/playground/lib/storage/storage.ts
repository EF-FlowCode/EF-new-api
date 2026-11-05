import { useAuthStore } from '@/stores/auth-store'
import { MESSAGE_STATUS, STORAGE_KEYS } from '../../constants'
import type {
  PlaygroundConfig,
  ParameterEnabled,
  Message,
  MessageAttachment,
  Session,
  SystemPreset,
} from '../../types'
import {
  finalizeMessage,
  isAssistantMessagePending,
  sanitizeMessagesOnLoad,
} from '../message/message-streaming-utils'
import { completeAssistantTiming } from '../message/message-timing-utils'
import { hasMessageContent } from '../message/message-utils'
import { createSessionStore, type SessionStore } from '../session/session-utils'
import {
  MAX_LOADED_MESSAGE_CHARS,
  MAX_LOADED_MESSAGES_CHARS,
  MAX_STORED_MESSAGES,
  MAX_STORED_MESSAGES_BYTES,
  STORAGE_VERSION,
  messagesSchema,
  parameterEnabledSchema,
  playgroundConfigSchema,
  sessionSchema,
  storedMessageSchema,
  systemPresetSchema,
} from './storage-schema'

type StoredEnvelope<T> = {
  version: number
  data: T
}

const TRUNCATED_CONTENT_SUFFIX = '\n\n[...]'
const MIN_PREFIX_COLLAPSE_LENGTH = 2000
const MIN_REPEATED_SECTION_COUNT = 3
const SECTION_HEADING_LINE_PATTERN = /^#{2,6}\s+\d+\.\s+.+$/gm

function getUserStorageKey(key: string): string | null {
  // Legacy data has no owner; never assign it to the next signed-in account.
  for (const legacyKey of Object.values(STORAGE_KEYS)) {
    localStorage.removeItem(legacyKey)
  }
  const userId = useAuthStore.getState().auth.user?.id
  return userId == null ? null : `${key}:${userId}`
}

function readStoredValue(baseKey: string): unknown | null {
  const key = getUserStorageKey(baseKey)
  if (!key) return null
  const saved = localStorage.getItem(key)
  if (!saved) return null

  return JSON.parse(saved) as unknown
}

function readStoredMessagesValue(): unknown | null {
  const key = getUserStorageKey(STORAGE_KEYS.MESSAGES)
  if (!key) return null
  const saved = localStorage.getItem(key)
  if (!saved) return null

  if (saved.length > MAX_STORED_MESSAGES_BYTES) {
    localStorage.removeItem(key)
    return null
  }

  return JSON.parse(saved) as unknown
}

function unwrapStoredValue(value: unknown): unknown {
  if (!value || typeof value !== 'object') {
    return value
  }

  if ('version' in value && 'data' in value) {
    return (value as StoredEnvelope<unknown>).data
  }

  return value
}

function writeStoredValue<T>(baseKey: string, data: T): void {
  const key = getUserStorageKey(baseKey)
  if (!key) return
  const payload: StoredEnvelope<T> = {
    version: STORAGE_VERSION,
    data,
  }

  localStorage.setItem(key, JSON.stringify(payload))
}

function trimMessages(messages: Message[]): Message[] {
  if (messages.length <= MAX_STORED_MESSAGES) {
    return messages
  }

  return messages.slice(-MAX_STORED_MESSAGES)
}

function getMessageSize(message: Message): number {
  const versionsSize = message.versions.reduce(
    (total, version) => total + version.content.length,
    0
  )
  const reasoningSize = message.reasoning?.content.length ?? 0

  return versionsSize + reasoningSize
}

function truncateText(text: string, maxLength: number): string {
  if (text.length <= maxLength) {
    return text
  }

  if (maxLength <= TRUNCATED_CONTENT_SUFFIX.length) {
    return text.slice(0, maxLength)
  }

  return `${text.slice(0, maxLength - TRUNCATED_CONTENT_SUFFIX.length)}${TRUNCATED_CONTENT_SUFFIX}`
}

type SectionOccurrence = {
  heading: string
  index: number
}

function getSectionOccurrences(text: string): SectionOccurrence[] {
  const occurrences: SectionOccurrence[] = []
  const matches = text.matchAll(SECTION_HEADING_LINE_PATTERN)
  for (const match of matches) {
    const index = match.index
    if (index === undefined) {
      continue
    }

    occurrences.push({
      heading: match[0],
      index,
    })
  }

  return occurrences
}

function getHeadingCounts(
  occurrences: SectionOccurrence[]
): Map<string, number> {
  const counts = new Map<string, number>()

  for (const occurrence of occurrences) {
    counts.set(occurrence.heading, (counts.get(occurrence.heading) ?? 0) + 1)
  }

  return counts
}

function findLastRepeatedSectionRunStart(text: string): number {
  const occurrences = getSectionOccurrences(text)
  const headingCounts = getHeadingCounts(occurrences)
  const lastRepeatedIndexes: number[] = []
  const seenHeadings = new Set<string>()

  for (let index = occurrences.length - 1; index >= 0; index--) {
    const occurrence = occurrences[index]
    const count = headingCounts.get(occurrence.heading) ?? 0

    if (
      count < MIN_REPEATED_SECTION_COUNT ||
      seenHeadings.has(occurrence.heading)
    ) {
      continue
    }

    seenHeadings.add(occurrence.heading)
    lastRepeatedIndexes.push(occurrence.index)
  }

  if (lastRepeatedIndexes.length === 0) {
    return -1
  }

  return Math.min(...lastRepeatedIndexes)
}

function collapseRepeatedSectionSnapshots(text: string): string {
  if (text.length < MIN_PREFIX_COLLAPSE_LENGTH) {
    return text
  }

  const lastRepeatedRunStart = findLastRepeatedSectionRunStart(text)
  if (lastRepeatedRunStart === -1) {
    return text
  }

  return text.slice(lastRepeatedRunStart)
}

function normalizeStoredMessageForLoad(message: Message): Message {
  let changed = false
  const versions = message.versions.map((version) => {
    const collapsedContent = collapseRepeatedSectionSnapshots(version.content)
    const content = truncateText(collapsedContent, MAX_LOADED_MESSAGE_CHARS)

    if (content === version.content && collapsedContent === version.content) {
      return version
    }

    changed = true
    return {
      ...version,
      content,
    }
  })

  const reasoning = message.reasoning
    ? {
        ...message.reasoning,
        content: truncateText(
          message.reasoning.content,
          MAX_LOADED_MESSAGE_CHARS
        ),
      }
    : undefined

  if (reasoning?.content !== message.reasoning?.content) {
    changed = true
  }

  const normalized = changed ? { ...message, versions, reasoning } : message

  if (!isAssistantMessagePending(normalized)) {
    return normalized
  }

  const hasContent = hasMessageContent(normalized)
  const hasReasoning = normalized.reasoning?.content.trim()

  if (!hasContent && !hasReasoning) {
    return normalized
  }

  const completedAt =
    normalized.completedAt ??
    normalized.reasoning?.completedAt ??
    normalized.startedAt ??
    normalized.createdAt ??
    Date.now()

  return completeAssistantTiming(
    {
      ...finalizeMessage(normalized),
      status: MESSAGE_STATUS.COMPLETE,
      isReasoningStreaming: false,
    },
    completedAt
  )
}

function trimMessagesByContentSize(messages: Message[]): Message[] {
  let totalSize = 0
  const result: Message[] = []

  for (let index = messages.length - 1; index >= 0; index--) {
    const message = messages[index]
    const messageSize = getMessageSize(message)

    if (
      result.length > 0 &&
      totalSize + messageSize > MAX_LOADED_MESSAGES_CHARS
    ) {
      break
    }

    totalSize += messageSize
    result.push(message)
  }

  return result.reverse()
}

/**
 * Load playground config from localStorage
 */
export function loadConfig(): Partial<PlaygroundConfig> {
  try {
    const saved = readStoredValue(STORAGE_KEYS.CONFIG)
    if (!saved) return {}

    return playgroundConfigSchema.parse(unwrapStoredValue(saved))
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
    const parsed = playgroundConfigSchema.parse(config)
    writeStoredValue(STORAGE_KEYS.CONFIG, parsed)
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
    const saved = readStoredValue(STORAGE_KEYS.PARAMETER_ENABLED)
    if (!saved) return {}

    return parameterEnabledSchema.parse(unwrapStoredValue(saved))
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
    const parsed = parameterEnabledSchema.parse(parameterEnabled)
    writeStoredValue(STORAGE_KEYS.PARAMETER_ENABLED, parsed)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save parameter enabled:', error)
  }
}

function splitLegacyContent(
  content: unknown,
  attachments: MessageAttachment[]
): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) return ''

  const texts: string[] = []
  for (const part of content as Array<Record<string, unknown>>) {
    if (part?.type === 'text' && typeof part.text === 'string') {
      texts.push(part.text)
    }
    const imageUrl = (part?.image_url as { url?: unknown } | undefined)?.url
    if (part?.type === 'image_url' && typeof imageUrl === 'string') {
      attachments.push({
        kind: 'image',
        name: `image-${attachments.length + 1}`,
        mediaType: imageUrl.match(/^data:([^;,]+)/)?.[1] ?? 'image/*',
        url: imageUrl,
      })
    }
  }
  return texts.join('\n')
}

/**
 * eflow 旧版消息：用户消息 content 可能是 [{type:'text'}, {type:'image_url'}]
 * 数组，统一成文本 + 图片附件。重新生成留下的多个版本照旧保留，当前版本
 * 是最后一个（与新版 activeVersion 缺省一致）。
 */
function migrateLegacyMessage(value: unknown): unknown {
  if (!value || typeof value !== 'object') return value
  const raw = value as Record<string, unknown>
  if (!Array.isArray(raw.versions) || raw.versions.length === 0) return value
  if (
    raw.versions.every(
      (version) =>
        typeof (version as { content?: unknown })?.content === 'string'
    )
  ) {
    return value
  }

  const attachments: MessageAttachment[] = []
  const lastIndex = raw.versions.length - 1
  const versions = raw.versions.map((version, index) => {
    const item = version as { id?: unknown; content?: unknown }
    // 附件只取当前版本，避免同一张图在多个版本里重复
    const text = splitLegacyContent(
      item?.content,
      index === lastIndex ? attachments : []
    )
    return {
      id: typeof item?.id === 'string' ? item.id : `v${index}`,
      content: text,
    }
  })

  return {
    ...raw,
    versions,
    ...(attachments.length > 0 ? { attachments } : {}),
  }
}

/**
 * Validate stored messages one by one; a corrupted entry is dropped instead
 * of discarding the whole conversation.
 */
function parseStoredMessages(values: unknown[]): Message[] {
  const messages: Message[] = []
  for (const value of values) {
    const parsed = storedMessageSchema.safeParse(migrateLegacyMessage(value))
    if (parsed.success) messages.push(parsed.data as Message)
  }
  return messages
}

function prepareLoadedMessages(messages: Message[]): Message[] {
  const normalized = messages.map(normalizeStoredMessageForLoad)
  return sanitizeMessagesOnLoad(
    trimMessagesByContentSize(trimMessages(normalized))
  )
}

/**
 * Messages saved by the single-conversation Playground, migrated into the
 * first session.
 */
function loadLegacyMessages(): Message[] {
  try {
    const saved = readStoredMessagesValue()
    if (!saved) return []
    const value = unwrapStoredValue(saved)
    return Array.isArray(value)
      ? prepareLoadedMessages(parseStoredMessages(value))
      : []
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load legacy messages:', error)
    return []
  }
}

/**
 * Load all sessions and the active one. Falls back to migrating the
 * single-conversation messages; always returns at least one session.
 */
export function loadSessions(): SessionStore {
  try {
    const saved = readStoredValue(STORAGE_KEYS.SESSIONS)
    const value = saved ? unwrapStoredValue(saved) : null
    if (Array.isArray(value)) {
      const sessions: Session[] = []
      for (const item of value) {
        const parsed = sessionSchema.safeParse(item)
        if (!parsed.success) continue
        sessions.push({
          ...parsed.data,
          messages: prepareLoadedMessages(
            parseStoredMessages(parsed.data.messages)
          ),
        })
      }

      if (sessions.length > 0) {
        const savedActive = readStoredActiveSessionId()
        const activeId = sessions.some((session) => session.id === savedActive)
          ? (savedActive as string)
          : sessions[0].id
        return { sessions, activeId }
      }
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load sessions:', error)
    // 解析失败时不回写，保留原始存储，避免用空会话覆盖全部历史
    return createSessionStore()
  }

  const store = createSessionStore(loadLegacyMessages())
  if (saveSessions(store) !== 'failed') {
    removeStoredValue(STORAGE_KEYS.MESSAGES)
  }
  return store
}

/**
 * eflow 旧版直接存裸字符串，新版存 envelope；两种都认。
 */
function readStoredActiveSessionId(): string | null {
  const key = getUserStorageKey(STORAGE_KEYS.ACTIVE_SESSION)
  if (!key) return null
  const saved = localStorage.getItem(key)
  if (!saved) return null

  try {
    const value = unwrapStoredValue(JSON.parse(saved))
    return typeof value === 'string' ? value : null
  } catch {
    return saved
  }
}

export type SaveSessionsResult = 'saved' | 'saved-without-files' | 'failed'

/**
 * Drop image / PDF data from stored history; names and extracted text stay.
 */
function stripAttachmentData(sessions: Session[]): Session[] {
  return sessions.map((session) => ({
    ...session,
    messages: session.messages.map((message) =>
      message.attachments?.some((attachment) => attachment.url)
        ? {
            ...message,
            attachments: message.attachments.map(
              ({ url: _url, ...attachment }) => attachment
            ),
          }
        : message
    ),
  }))
}

/**
 * Persist all sessions. When the browser storage quota is exceeded (large
 * images / PDFs), retry without file data so the text history is still kept.
 */
export function saveSessions(store: SessionStore): SaveSessionsResult {
  let sessions: Session[]
  try {
    sessions = store.sessions.map((session) => ({
      ...session,
      messages: messagesSchema.parse(
        trimMessages(session.messages)
      ) as Message[],
    }))
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save sessions:', error)
    return 'failed'
  }

  try {
    writeStoredValue(STORAGE_KEYS.SESSIONS, sessions)
    writeStoredValue(STORAGE_KEYS.ACTIVE_SESSION, store.activeId)
    return 'saved'
  } catch (error) {
    try {
      writeStoredValue(STORAGE_KEYS.SESSIONS, stripAttachmentData(sessions))
      writeStoredValue(STORAGE_KEYS.ACTIVE_SESSION, store.activeId)
      return 'saved-without-files'
    } catch {
      // eslint-disable-next-line no-console
      console.error('Failed to save sessions:', error)
      return 'failed'
    }
  }
}

/**
 * Load system prompt presets (shared by all sessions)
 */
export function loadSystemPresets(): SystemPreset[] {
  try {
    const saved = readStoredValue(STORAGE_KEYS.SYSTEM_PRESETS)
    const value = saved ? unwrapStoredValue(saved) : null
    if (!Array.isArray(value)) return []
    return value.flatMap((item) => {
      const parsed = systemPresetSchema.safeParse(item)
      return parsed.success ? [parsed.data] : []
    })
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to load system presets:', error)
  }
  return []
}

export function saveSystemPresets(presets: SystemPreset[]): void {
  try {
    writeStoredValue(STORAGE_KEYS.SYSTEM_PRESETS, presets)
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to save system presets:', error)
  }
}

function removeStoredValue(baseKey: string): void {
  const key = getUserStorageKey(baseKey)
  if (key) localStorage.removeItem(key)
}

/**
 * Clear the signed-in user's playground data
 */
export function clearPlaygroundData(): void {
  try {
    for (const baseKey of Object.values(STORAGE_KEYS)) {
      removeStoredValue(baseKey)
    }
  } catch (error) {
    // eslint-disable-next-line no-console
    console.error('Failed to clear playground data:', error)
  }
}
