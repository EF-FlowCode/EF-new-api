import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { Message } from '../types'
import { sessionToMarkdown } from './export-session'
import { isValidMessage } from './message-utils'
import { estimateConversationUsage } from './token-estimate'

const regenerated: Message = {
  key: 'a1',
  from: 'assistant',
  versions: [
    { id: 'v1', content: 'old answer' },
    { id: 'v2', content: 'new answer that is longer' },
  ],
} as Message

describe('playground regenerated messages', () => {
  test('export uses the current (latest) version', () => {
    const md = sessionToMarkdown({
      id: 's1',
      title: 't',
      messages: [regenerated],
      createdAt: 0,
      updatedAt: 0,
    } as never)

    assert.ok(md.includes('new answer that is longer'))
    assert.ok(!md.includes('old answer'))
  })

  test('an empty current version is not sent even if an older one has text', () => {
    const pending = {
      ...regenerated,
      versions: [...regenerated.versions, { id: 'v3', content: '' }],
    } as Message

    assert.equal(isValidMessage(pending), false)
  })

  test('token estimate counts the current version', () => {
    const single = {
      ...regenerated,
      versions: [regenerated.versions[1]],
    } as Message

    assert.equal(
      estimateConversationUsage([regenerated]).tokens,
      estimateConversationUsage([single]).tokens
    )
  })
})
