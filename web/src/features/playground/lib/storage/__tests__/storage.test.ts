import assert from 'node:assert/strict'
import { afterEach, beforeEach, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { STORAGE_KEYS } from '../../../constants'
import type { Message } from '../../../types'
import { getMessageContent } from '../../message/message-utils'
import { createSessionStore } from '../../session/session-utils'
import {
  clearPlaygroundData,
  loadConfig,
  loadParameterEnabled,
  loadSessions,
  loadSystemPresets,
  saveConfig,
  saveParameterEnabled,
  saveSessions,
  saveSystemPresets,
} from '../storage'

class MemoryStorage {
  private data = new Map<string, string>()
  quotaBytes = Number.POSITIVE_INFINITY

  get length() {
    return this.data.size
  }
  key(index: number) {
    return [...this.data.keys()][index] ?? null
  }
  getItem(key: string) {
    return this.data.get(key) ?? null
  }
  setItem(key: string, value: string) {
    if (String(value).length > this.quotaBytes) {
      throw new Error('QuotaExceededError')
    }
    this.data.set(key, String(value))
  }
  removeItem(key: string) {
    this.data.delete(key)
  }
  clear() {
    this.data.clear()
  }
}

const storage = new MemoryStorage()
Object.defineProperty(globalThis, 'localStorage', {
  value: storage,
  configurable: true,
})

const messages: Message[] = [
  {
    key: 'a-chat',
    from: 'user',
    versions: [{ id: 'a-version', content: 'Account A private chat' }],
  },
]

function signIn(id: number) {
  useAuthStore.getState().auth.setUser({ id, username: `u${id}`, role: 1 })
}

function signOut() {
  useAuthStore.getState().auth.reset()
}

beforeEach(() => {
  storage.clear()
  storage.quotaBytes = Number.POSITIVE_INFINITY
  signOut()
})

afterEach(() => {
  storage.clear()
  signOut()
})

test('switching accounts isolates config, sessions, presets and parameter toggles', () => {
  signIn(1)
  saveConfig({ model: 'account-a-model' })
  saveSessions(createSessionStore(messages))
  saveParameterEnabled({ temperature: true })
  saveSystemPresets([{ id: 'p1', name: 'A preset', content: 'Be brief' }])

  signOut()
  signIn(2)
  assert.deepEqual(loadConfig(), {})
  assert.deepEqual(loadSessions().sessions[0].messages, [])
  assert.deepEqual(loadParameterEnabled(), {})
  assert.deepEqual(loadSystemPresets(), [])
  saveConfig({ model: 'account-b-model' })

  signIn(1)
  assert.deepEqual(loadConfig(), { model: 'account-a-model' })
  assert.deepEqual(loadSessions().sessions[0].messages, messages)
  assert.deepEqual(loadParameterEnabled(), { temperature: true })
  assert.equal(loadSystemPresets()[0]?.name, 'A preset')

  clearPlaygroundData()
  assert.deepEqual(loadConfig(), {})
  assert.deepEqual(loadSystemPresets(), [])
  signIn(2)
  assert.deepEqual(loadConfig(), { model: 'account-b-model' })
})

test('legacy global data is removed instead of assigned to the first signed-in account', () => {
  storage.setItem(STORAGE_KEYS.CONFIG, JSON.stringify({ model: 'legacy' }))
  storage.setItem(STORAGE_KEYS.MESSAGES, JSON.stringify(messages))
  storage.setItem(STORAGE_KEYS.SESSIONS, JSON.stringify([]))
  signIn(2)

  assert.deepEqual(loadConfig(), {})
  assert.deepEqual(loadSessions().sessions[0].messages, [])
  for (const key of Object.values(STORAGE_KEYS)) {
    assert.equal(storage.getItem(key), null)
  }
})

test('storage calls without a signed-in user do not persist shared data', () => {
  saveConfig({ model: 'signed-out-model' })
  saveSessions(createSessionStore(messages))
  saveParameterEnabled({ temperature: true })
  saveSystemPresets([{ id: 'p1', name: 'x', content: 'y' }])

  assert.deepEqual(loadConfig(), {})
  assert.equal(storage.length, 0)
})

test('single-conversation messages migrate into the first session', () => {
  signIn(3)
  storage.setItem(
    `${STORAGE_KEYS.MESSAGES}:3`,
    JSON.stringify({ version: 1, data: messages })
  )

  const store = loadSessions()
  assert.equal(store.sessions.length, 1)
  assert.deepEqual(store.sessions[0].messages, messages)
  assert.equal(store.sessions[0].title, 'Account A private chat')
  assert.equal(storage.getItem(`${STORAGE_KEYS.MESSAGES}:3`), null)
  assert.deepEqual(loadSessions().sessions[0].messages, messages)
})

test('previous eflow sessions keep regenerated versions and multimodal images', () => {
  signIn(4)
  storage.setItem(
    `${STORAGE_KEYS.SESSIONS}:4`,
    JSON.stringify([
      {
        id: 's1',
        title: 'Old chat',
        createdAt: 1,
        updatedAt: 2,
        systemPrompt: 'Be brief',
        messages: [
          {
            key: 'u1',
            from: 'user',
            versions: [
              {
                id: 'v1',
                content: [
                  { type: 'text', text: 'What is this?' },
                  {
                    type: 'image_url',
                    image_url: { url: 'data:image/png;base64,AAAA' },
                  },
                ],
              },
            ],
          },
          {
            key: 'a1',
            from: 'assistant',
            status: 'complete',
            streamingVisible: 'transient',
            versions: [
              { id: 'old', content: 'First answer' },
              { id: 'new', content: 'Regenerated answer' },
            ],
          },
          { broken: true },
        ],
      },
    ])
  )
  // 旧版直接存裸字符串
  storage.setItem(`${STORAGE_KEYS.ACTIVE_SESSION}:4`, 's1')

  const store = loadSessions()
  assert.equal(store.activeId, 's1')
  const [session] = store.sessions
  assert.equal(session.systemPrompt, 'Be brief')
  assert.equal(session.messages.length, 2)
  assert.deepEqual(session.messages[0].versions, [
    { id: 'v1', content: 'What is this?' },
  ])
  assert.deepEqual(session.messages[0].attachments, [
    {
      kind: 'image',
      name: 'image-1',
      mediaType: 'image/png',
      url: 'data:image/png;base64,AAAA',
    },
  ])
  assert.deepEqual(session.messages[1].versions, [
    { id: 'old', content: 'First answer' },
    { id: 'new', content: 'Regenerated answer' },
  ])
  assert.equal(getMessageContent(session.messages[1]), 'Regenerated answer')
  assert.equal('streamingVisible' in session.messages[1], false)
})

test('answer versions and their state survive a save and reload', () => {
  signIn(6)
  const store = createSessionStore([
    {
      key: 'a1',
      from: 'assistant',
      status: 'complete',
      durationMs: 1200,
      activeVersion: 0,
      versions: [
        { id: 'v1', content: 'First' },
        {
          id: 'v2',
          content: 'Second',
          state: {
            status: 'complete',
            durationMs: 3400,
            reasoning: { content: 'Thought', duration: 2 },
          },
        },
      ],
    },
  ])

  assert.equal(saveSessions(store), 'saved')
  const [message] = loadSessions().sessions[0].messages
  assert.equal(message.activeVersion, 0)
  assert.equal(getMessageContent(message), 'First')
  assert.deepEqual(message.versions[1].state, {
    status: 'complete',
    durationMs: 3400,
    reasoning: { content: 'Thought', duration: 2 },
  })
})

test('when storage is full, file data is dropped but text history is kept', () => {
  signIn(5)
  const bigImage = `data:image/png;base64,${'A'.repeat(5000)}`
  const store = createSessionStore([
    {
      key: 'u1',
      from: 'user',
      versions: [{ id: 'v1', content: 'Look at this' }],
      attachments: [
        { kind: 'image', name: 'a.png', mediaType: 'image/png', url: bigImage },
        {
          kind: 'text',
          name: 'notes.txt',
          mediaType: 'text/plain',
          text: 'hi',
        },
      ],
    },
  ])
  storage.quotaBytes = 2000

  assert.equal(saveSessions(store), 'saved-without-files')
  const [message] = loadSessions().sessions[0].messages
  assert.equal(message.versions[0].content, 'Look at this')
  assert.deepEqual(message.attachments, [
    { kind: 'image', name: 'a.png', mediaType: 'image/png' },
    { kind: 'text', name: 'notes.txt', mediaType: 'text/plain', text: 'hi' },
  ])
})
