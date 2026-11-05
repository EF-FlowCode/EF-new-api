import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { parseStreamMessageUpdates } from '../../streaming/stream-utils'
import {
  applyChatCompletionResponse,
  applyStreamingChunk,
  completeAssistantMessage,
} from '../message-streaming-utils'
import { createLoadingAssistantMessage } from '../message-utils'

const reasoningCases = [
  {
    name: 'preserves reasoning_content',
    fields: { reasoning_content: 'Thought' },
    expected: 'Thought',
  },
  {
    name: 'accepts reasoning when reasoning_content is absent',
    fields: { reasoning: 'Thought' },
    expected: 'Thought',
  },
  {
    name: 'accepts reasoning when reasoning_content is null',
    fields: { reasoning_content: null, reasoning: 'Thought' },
    expected: 'Thought',
  },
  {
    name: 'prefers reasoning_content without appending reasoning',
    fields: { reasoning_content: 'Thought', reasoning: 'Other thought' },
    expected: 'Thought',
  },
  {
    name: 'preserves an explicitly empty reasoning_content',
    fields: { reasoning_content: '', reasoning: 'Other thought' },
    expected: undefined,
  },
  {
    name: 'keeps content-only responses free of reasoning',
    fields: {},
    expected: undefined,
  },
  {
    name: 'ignores null reasoning fields',
    fields: { reasoning_content: null, reasoning: null },
    expected: undefined,
  },
  {
    name: 'preserves think-tag extraction without reasoning fields',
    fields: { content: '<think>Thought</think>Answer' },
    expected: 'Thought',
  },
]

describe('chat completion reasoning', () => {
  for (const { name, fields, expected } of reasoningCases) {
    test(`streaming: ${name}`, () => {
      const updates = parseStreamMessageUpdates(
        JSON.stringify({
          choices: [{ delta: { content: 'Answer', ...fields } }],
        })
      )
      let message = createLoadingAssistantMessage(0)

      for (const update of updates) {
        message = applyStreamingChunk(message, update.type, update.chunk)
      }
      const completed = completeAssistantMessage(message)

      assert.equal(completed.reasoning?.content, expected)
      assert.equal(completed.versions[0].content, 'Answer')
      assert.equal(completed.status, 'complete')
      assert.equal(completed.isReasoningStreaming, false)
    })
  }

  for (const { name, fields, expected } of reasoningCases) {
    test(`non-streaming: ${name}`, () => {
      const completed = applyChatCompletionResponse(
        createLoadingAssistantMessage(0),
        {
          id: 'chatcmpl-test',
          object: 'chat.completion',
          created: 0,
          model: 'test-model',
          choices: [
            {
              index: 0,
              message: { role: 'assistant', content: 'Answer', ...fields },
              finish_reason: 'stop',
            },
          ],
        }
      )

      assert.notEqual(completed, null)
      assert.equal(completed?.reasoning?.content, expected)
      assert.equal(completed?.versions[0].content, 'Answer')
      assert.equal(completed?.status, 'complete')
      assert.equal(completed?.isReasoningStreaming, false)
    })
  }

  test('emits reasoning before content when both occur in one delta', () => {
    const updates = parseStreamMessageUpdates(
      JSON.stringify({
        choices: [{ delta: { reasoning: 'Thought', content: 'Answer' } }],
      })
    )

    assert.deepEqual(updates, [
      { type: 'reasoning', chunk: 'Thought' },
      { type: 'content', chunk: 'Answer' },
    ])
  })
})
