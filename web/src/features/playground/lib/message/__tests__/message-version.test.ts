import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { Message } from '../../../types'
import { buildChatCompletionPayload } from '../../streaming/payload-builder'
import {
  createRegeneratedMessages,
  switchMessageVersionByKey,
} from '../conversation-message-utils'
import {
  applyStreamingChunk,
  completeAssistantMessage,
} from '../message-streaming-utils'
import { updateAssistantMessageWithError } from '../message-update-utils'
import { getCurrentVersion, getMessageContent } from '../message-utils'
import { canSwitchVersions } from '../message-version-utils'

const CONFIG = {
  model: 'm',
  group: 'default',
  temperature: 1,
  top_p: 1,
  max_tokens: 1,
  frequency_penalty: 0,
  presence_penalty: 0,
  seed: null,
  stream: true,
}

const PARAMETERS_OFF = {
  temperature: false,
  top_p: false,
  max_tokens: false,
  frequency_penalty: false,
  presence_penalty: false,
  seed: false,
}

function conversation(): Message[] {
  return [
    { key: 'u1', from: 'user', versions: [{ id: 'q', content: 'Question' }] },
    {
      key: 'a1',
      from: 'assistant',
      versions: [{ id: 'v1', content: 'First answer' }],
      status: 'complete',
      durationMs: 1000,
      reasoning: { content: 'First thought', duration: 1 },
    },
  ]
}

function stream(messages: Message[], text: string): Message[] {
  const last = messages.at(-1)!
  const updated = completeAssistantMessage(
    applyStreamingChunk(last, 'content', text)
  )
  return [...messages.slice(0, -1), updated]
}

describe('regenerated answer versions', () => {
  test('regenerating keeps the old answer as a version with its own state', () => {
    const regenerated = createRegeneratedMessages(conversation(), 'a1')!
    const answer = regenerated[1]

    assert.equal(answer.key, 'a1')
    assert.equal(answer.versions.length, 2)
    assert.equal(answer.activeVersion, 1)
    assert.equal(answer.status, 'loading')
    assert.equal(answer.reasoning, undefined)
    assert.equal(answer.versions[0].state?.durationMs, 1000)
    assert.equal(answer.versions[0].state?.reasoning?.content, 'First thought')

    // The old answer is not sent back as context for the new one.
    const payload = buildChatCompletionPayload(
      regenerated,
      CONFIG,
      PARAMETERS_OFF
    )
    assert.deepEqual(payload.messages, [{ role: 'user', content: 'Question' }])

    const done = stream(regenerated, 'Second answer')
    assert.equal(getMessageContent(done[1]), 'Second answer')
    assert.equal(done[1].versions[0].content, 'First answer')
    assert.ok(canSwitchVersions(done[1]))
  })

  test('switching versions restores each answer and its state', () => {
    let messages = stream(
      createRegeneratedMessages(conversation(), 'a1')!,
      'Second answer'
    )
    const secondDuration = messages[1].durationMs

    messages = switchMessageVersionByKey(messages, 'a1', 0)
    assert.equal(getMessageContent(messages[1]), 'First answer')
    assert.equal(messages[1].durationMs, 1000)
    assert.equal(messages[1].reasoning?.content, 'First thought')
    assert.equal(messages[1].versions[0].state, undefined)

    messages = switchMessageVersionByKey(messages, 'a1', 1)
    assert.equal(getMessageContent(messages[1]), 'Second answer')
    assert.equal(messages[1].durationMs, secondDuration)
    assert.equal(messages[1].reasoning, undefined)

    // Out-of-range indexes are ignored.
    assert.equal(switchMessageVersionByKey(messages, 'a1', 5)[1], messages[1])
  })

  test('the chosen version is the one sent as context afterwards', () => {
    let messages = stream(
      createRegeneratedMessages(conversation(), 'a1')!,
      'Second answer'
    )
    messages = switchMessageVersionByKey(messages, 'a1', 0)
    messages = [
      ...messages,
      {
        key: 'u2',
        from: 'user',
        versions: [{ id: 'q2', content: 'Follow-up' }],
      },
    ]

    const payload = buildChatCompletionPayload(messages, CONFIG, PARAMETERS_OFF)
    assert.deepEqual(payload.messages[1], {
      role: 'assistant',
      content: 'First answer',
    })
  })

  test('a failed regeneration is replaced instead of kept', () => {
    const failed = updateAssistantMessageWithError(
      createRegeneratedMessages(conversation(), 'a1')!,
      'boom'
    )
    assert.equal(failed[1].versions.length, 2)
    assert.equal(failed[1].status, 'error')

    const retried = createRegeneratedMessages(failed, 'a1')!
    assert.equal(retried[1].versions.length, 2)
    assert.equal(retried[1].versions[0].content, 'First answer')
    assert.equal(getCurrentVersion(retried[1]).content, '')

    // The old answer is still reachable after the failure.
    const back = switchMessageVersionByKey(failed, 'a1', 0)
    assert.equal(getMessageContent(back[1]), 'First answer')
    assert.equal(back[1].status, 'complete')
    assert.equal(back[1].errorCode, undefined)
  })

  test('regenerating from the user message adds a version to its answer', () => {
    const messages = [
      ...conversation(),
      { key: 'u2', from: 'user', versions: [{ id: 'q2', content: 'Later' }] },
    ] satisfies Message[]

    const regenerated = createRegeneratedMessages(messages, 'u1')!
    assert.deepEqual(
      regenerated.map((message) => message.key),
      ['u1', 'a1']
    )
    assert.equal(regenerated[1].versions.length, 2)
  })
})
