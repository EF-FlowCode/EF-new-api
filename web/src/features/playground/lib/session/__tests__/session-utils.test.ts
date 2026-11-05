import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { DEFAULT_SESSION_TITLE } from '../../../constants'
import type { Message } from '../../../types'
import { buildChatCompletionPayload } from '../../streaming/payload-builder'
import {
  addSession,
  createSessionStore,
  deriveSessionTitle,
  getActiveSession,
  removeSession,
  renameSession,
  sessionMatchesQuery,
  setActiveSystemPrompt,
  switchSession,
  updateActiveSessionMessages,
} from '../session-utils'
import { estimateConversationUsage, trimConversation } from '../token-estimate'

function userMessage(key: string, content: string): Message {
  return { key, from: 'user', versions: [{ id: `${key}-v`, content }] }
}

describe('playground sessions', () => {
  test('derives the title from the first user message until renamed', () => {
    let store = createSessionStore()
    assert.equal(getActiveSession(store)?.title, DEFAULT_SESSION_TITLE)

    store = updateActiveSessionMessages(store, [
      userMessage('u1', '  Explain   blue-green\ndeploys  '),
    ])
    assert.equal(getActiveSession(store)?.title, 'Explain blue-green deploys')

    store = renameSession(store, store.activeId, 'Deploy notes')
    store = updateActiveSessionMessages(store, [userMessage('u2', 'Other')])
    assert.equal(getActiveSession(store)?.title, 'Deploy notes')
    assert.equal(
      deriveSessionTitle([userMessage('x', 'a'.repeat(40))]).length,
      31
    )
  })

  test('reuses an empty session and keeps at least one session', () => {
    const empty = createSessionStore()
    assert.equal(addSession(empty), empty)

    const withMessages = updateActiveSessionMessages(empty, [
      userMessage('u1', 'hi'),
    ])
    const two = addSession(withMessages)
    assert.equal(two.sessions.length, 2)
    assert.notEqual(two.activeId, withMessages.activeId)
    assert.equal(
      switchSession(two, withMessages.activeId).activeId,
      withMessages.activeId
    )
    assert.equal(switchSession(two, 'missing'), two)

    const one = removeSession(two, two.activeId)
    assert.equal(one.sessions.length, 1)
    assert.equal(one.activeId, withMessages.activeId)
    const fresh = removeSession(one, one.activeId)
    assert.equal(fresh.sessions.length, 1)
    assert.deepEqual(fresh.sessions[0].messages, [])
  })

  test('search matches titles and message text case-insensitively', () => {
    const store = updateActiveSessionMessages(createSessionStore(), [
      userMessage('u1', 'Hello Rolling Deploy'),
    ])
    const session = getActiveSession(store)!
    assert.ok(sessionMatchesQuery(session, 'rolling'))
    assert.ok(sessionMatchesQuery(session, ''))
    assert.ok(!sessionMatchesQuery(session, 'mysql'))
  })
})

describe('system prompt and context usage', () => {
  test('injects the session system prompt as the first message once', () => {
    const store = setActiveSystemPrompt(createSessionStore(), ' Be brief ')
    const payload = buildChatCompletionPayload(
      [userMessage('u1', 'hi')],
      {
        model: 'm',
        group: 'default',
        temperature: 1,
        top_p: 1,
        max_tokens: 1,
        frequency_penalty: 0,
        presence_penalty: 0,
        seed: null,
        stream: true,
      },
      {
        temperature: false,
        top_p: false,
        max_tokens: false,
        frequency_penalty: false,
        presence_penalty: false,
        seed: false,
      },
      getActiveSession(store)?.systemPrompt
    )
    assert.deepEqual(payload.messages, [
      { role: 'system', content: 'Be brief' },
      { role: 'user', content: 'hi' },
    ])
  })

  test('estimates tokens including extracted file text and counts other files', () => {
    const usage = estimateConversationUsage(
      [
        {
          ...userMessage('u1', '你好'),
          attachments: [
            {
              kind: 'text',
              name: 'a.txt',
              mediaType: 'text/plain',
              text: 'abcd',
            },
            {
              kind: 'file',
              name: 'a.pdf',
              mediaType: 'application/pdf',
              url: 'data:',
            },
          ],
        },
      ],
      'abcdefgh'
    )
    // system: 2 + 4, message: 2 + 4, text attachment: 1
    assert.deepEqual(usage, { tokens: 13, messages: 1, files: 1 })
  })

  test('trimming keeps system messages and the most recent messages', () => {
    const messages: Message[] = [
      { key: 's', from: 'system', versions: [{ id: 's', content: 'sys' }] },
      ...Array.from({ length: 5 }, (_, index) =>
        userMessage(`u${index}`, `m${index}`)
      ),
    ]
    assert.deepEqual(
      trimConversation(messages, 2).map((message) => message.key),
      ['s', 'u3', 'u4']
    )
  })
})
