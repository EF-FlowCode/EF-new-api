import assert from 'node:assert/strict'
import { afterEach, beforeEach, test } from 'node:test'
import { useAuthStore } from '@/stores/auth-store'
import { STORAGE_KEYS } from '../constants'
import type { Message } from '../types'
import {
  clearPlaygroundData,
  loadConfig,
  loadMessages,
  loadParameterEnabled,
  loadSessions,
  loadSystemPresets,
  saveConfig,
  saveMessages,
  saveParameterEnabled,
  saveSessions,
  saveSystemPresets,
} from './storage'

class MemoryStorage {
  private data = new Map<string, string>()
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

beforeEach(() => {
  storage.clear()
  useAuthStore.getState().auth.reset()
})

afterEach(() => {
  storage.clear()
  useAuthStore.getState().auth.reset()
})

test('切换账号时配置、会话、参数开关与提示词预设互相隔离，切回后原数据仍在', () => {
  signIn(1)
  saveConfig({ model: 'account-a-model' })
  saveMessages(messages)
  saveParameterEnabled({ temperature: true })
  saveSessions(
    [{ id: 's-a', title: 'A', createdAt: 1, updatedAt: 1, messages }],
    's-a'
  )
  saveSystemPresets([{ id: 'p-a', name: 'A', content: 'secret' }])

  useAuthStore.getState().auth.reset()
  signIn(2)
  assert.deepEqual(loadConfig(), {})
  assert.equal(loadMessages(), null)
  assert.deepEqual(loadParameterEnabled(), {})
  assert.deepEqual(loadSystemPresets(), [])
  const b = loadSessions()
  assert.equal(b.sessions.length, 1)
  assert.deepEqual(b.sessions[0].messages, [])
  saveConfig({ model: 'account-b-model' })
  saveParameterEnabled({ temperature: false })

  signIn(1)
  assert.deepEqual(loadConfig(), { model: 'account-a-model' })
  assert.deepEqual(loadMessages(), messages)
  assert.deepEqual(loadParameterEnabled(), { temperature: true })
  assert.deepEqual(loadSystemPresets(), [
    { id: 'p-a', name: 'A', content: 'secret' },
  ])
  const a = loadSessions()
  assert.equal(a.activeId, 's-a')
  assert.deepEqual(a.sessions[0].messages, messages)

  clearPlaygroundData()
  assert.equal(loadMessages(), null)
  signIn(2)
  assert.deepEqual(loadConfig(), { model: 'account-b-model' })
  assert.deepEqual(loadParameterEnabled(), { temperature: false })
})

test('旧版固定键下的共享数据直接删除，不分给第一个登录的账号', () => {
  storage.setItem(STORAGE_KEYS.CONFIG, JSON.stringify({ model: 'legacy' }))
  storage.setItem(STORAGE_KEYS.MESSAGES, JSON.stringify(messages))
  storage.setItem(
    STORAGE_KEYS.PARAMETER_ENABLED,
    JSON.stringify({ temperature: true })
  )
  storage.setItem(
    STORAGE_KEYS.SESSIONS,
    JSON.stringify([
      { id: 'legacy', title: 'L', createdAt: 1, updatedAt: 1, messages },
    ])
  )
  storage.setItem(STORAGE_KEYS.ACTIVE_SESSION, 'legacy')
  storage.setItem(
    STORAGE_KEYS.SYSTEM_PRESETS,
    JSON.stringify([{ id: 'p', name: 'P', content: 'c' }])
  )
  signIn(2)

  assert.deepEqual(loadConfig(), {})
  assert.equal(loadMessages(), null)
  assert.deepEqual(loadParameterEnabled(), {})
  assert.deepEqual(loadSystemPresets(), [])
  assert.deepEqual(loadSessions().sessions[0].messages, [])
  for (const key of Object.values(STORAGE_KEYS)) {
    assert.equal(storage.getItem(key), null)
  }
})

test('未登录时既不读也不写共享数据', () => {
  saveConfig({ model: 'signed-out' })
  saveMessages(messages)
  saveParameterEnabled({ temperature: true })
  saveSystemPresets([{ id: 'p', name: 'P', content: 'c' }])
  loadSessions()
  assert.deepEqual(loadConfig(), {})
  assert.equal(loadMessages(), null)
  assert.deepEqual(loadParameterEnabled(), {})
  assert.equal(storage.length, 0)
})
