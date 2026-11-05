import { nanoid } from 'nanoid'
import { DEFAULT_SESSION_TITLE, MESSAGE_ROLES } from '../../constants'
import type { Message, Session } from '../../types'
import { getMessageContent } from '../message/message-utils'

const MAX_SESSION_TITLE_LENGTH = 30

export type SessionStore = {
  sessions: Session[]
  activeId: string
}

/**
 * Title from the first user message (one line, at most 30 chars).
 */
export function deriveSessionTitle(messages: Message[]): string {
  const firstUser = messages.find(
    (message) => message.from === MESSAGE_ROLES.USER
  )
  const text = firstUser
    ? getMessageContent(firstUser).replace(/\s+/g, ' ').trim()
    : ''
  if (!text) return DEFAULT_SESSION_TITLE

  return text.length > MAX_SESSION_TITLE_LENGTH
    ? `${text.slice(0, MAX_SESSION_TITLE_LENGTH)}…`
    : text
}

export function createSession(
  messages: Message[] = [],
  now: number = Date.now()
): Session {
  return {
    id: nanoid(),
    title: deriveSessionTitle(messages),
    createdAt: now,
    updatedAt: now,
    messages,
  }
}

export function createSessionStore(messages: Message[] = []): SessionStore {
  const session = createSession(messages)
  return { sessions: [session], activeId: session.id }
}

export function getActiveSession(store: SessionStore): Session | undefined {
  return (
    store.sessions.find((session) => session.id === store.activeId) ??
    store.sessions[0]
  )
}

/**
 * Replace the active session's messages. The title is derived only while the
 * session still carries the placeholder title, so manual renames stick.
 */
export function updateActiveSessionMessages(
  store: SessionStore,
  messages: Message[],
  now: number = Date.now()
): SessionStore {
  const active = getActiveSession(store)
  if (!active) return store

  return {
    ...store,
    sessions: store.sessions.map((session) =>
      session.id === active.id
        ? {
            ...session,
            messages,
            updatedAt: now,
            title:
              session.title === DEFAULT_SESSION_TITLE
                ? deriveSessionTitle(messages)
                : session.title,
          }
        : session
    ),
  }
}

/**
 * Start a new session. An empty active session is reused instead of piling
 * up blank sessions.
 */
export function addSession(store: SessionStore): SessionStore {
  const active = getActiveSession(store)
  if (active && active.messages.length === 0) return store

  const session = createSession()
  return { sessions: [session, ...store.sessions], activeId: session.id }
}

export function switchSession(store: SessionStore, id: string): SessionStore {
  if (id === store.activeId) return store
  if (!store.sessions.some((session) => session.id === id)) return store
  return { ...store, activeId: id }
}

/**
 * Delete a session; the store always keeps at least one session.
 */
export function removeSession(store: SessionStore, id: string): SessionStore {
  const remaining = store.sessions.filter((session) => session.id !== id)
  if (remaining.length === 0) return createSessionStore()

  const activeId =
    store.activeId === id ? getSortedSessions(remaining)[0].id : store.activeId
  return { sessions: remaining, activeId }
}

export function renameSession(
  store: SessionStore,
  id: string,
  title: string,
  now: number = Date.now()
): SessionStore {
  const nextTitle = title.trim()
  if (!nextTitle) return store

  return {
    ...store,
    sessions: store.sessions.map((session) =>
      session.id === id
        ? { ...session, title: nextTitle, updatedAt: now }
        : session
    ),
  }
}

export function setActiveSystemPrompt(
  store: SessionStore,
  systemPrompt: string,
  now: number = Date.now()
): SessionStore {
  const active = getActiveSession(store)
  if (!active) return store

  return {
    ...store,
    sessions: store.sessions.map((session) =>
      session.id === active.id
        ? { ...session, systemPrompt, updatedAt: now }
        : session
    ),
  }
}

/**
 * Sessions ordered for the sidebar: most recently updated first.
 */
export function getSortedSessions(sessions: Session[]): Session[] {
  return [...sessions].sort((a, b) => b.updatedAt - a.updatedAt)
}

/**
 * Case-insensitive match on the title and every message's text.
 */
export function sessionMatchesQuery(session: Session, query: string): boolean {
  const q = query.trim().toLowerCase()
  if (!q) return true
  if (session.title.toLowerCase().includes(q)) return true
  return session.messages.some((message) =>
    getMessageContent(message).toLowerCase().includes(q)
  )
}
