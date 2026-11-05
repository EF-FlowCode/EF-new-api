import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  defaultProtocolSelection,
  describeProtocolChanges,
  normalizeUpstreamProtocols,
  protocolRouteOutcome,
  supportsProtocolRouting,
} from './channel-protocols'

describe('channel protocols', () => {
  test('pre-selects only protocols the probe confirmed', () => {
    assert.deepEqual(
      defaultProtocolSelection({
        probed_at: 1,
        model: 'm',
        items: [
          { protocol: 'responses', verdict: 'supported', traffic_count: 0 },
          {
            protocol: 'messages',
            verdict: 'unknown',
            reason: 'client_restricted',
            traffic_count: 9,
          },
          { protocol: 'chat', verdict: 'supported', traffic_count: 0 },
        ],
      }),
      ['chat', 'responses']
    )
    assert.deepEqual(defaultProtocolSelection(undefined), [])
  })

  test('routes undeclared protocols through Chat unless chat is off or bodies pass through', () => {
    assert.equal(
      protocolRouteOutcome('messages', ['messages', 'chat'], false),
      'native'
    )
    assert.equal(
      protocolRouteOutcome('responses', ['messages', 'chat'], false),
      'chat_fallback'
    )
    assert.equal(
      protocolRouteOutcome('responses', ['messages', 'chat'], true),
      'rejected'
    )
    assert.equal(protocolRouteOutcome('chat', ['messages'], false), 'rejected')
  })

  test('Anthropic channels without chat convert Chat requests to Messages', () => {
    assert.equal(
      protocolRouteOutcome('chat', ['messages'], false, 14),
      'messages_fallback'
    )
    assert.equal(
      protocolRouteOutcome('responses', ['messages'], false, 14),
      'rejected'
    )
    assert.equal(
      protocolRouteOutcome('chat', ['messages'], true, 14),
      'rejected'
    )
    assert.equal(
      protocolRouteOutcome('chat', ['messages'], false, 1),
      'rejected'
    )
  })

  test('describes per-protocol changes against the saved selection', () => {
    assert.deepEqual(describeProtocolChanges([], ['messages', 'chat'], false), [
      { protocol: 'messages', before: 'legacy', after: 'native' },
      { protocol: 'chat', before: 'legacy', after: 'native' },
      { protocol: 'responses', before: 'legacy', after: 'chat_fallback' },
    ])
    assert.deepEqual(
      describeProtocolChanges(
        ['messages', 'chat', 'responses'],
        ['messages', 'chat'],
        false
      ),
      [{ protocol: 'responses', before: 'native', after: 'chat_fallback' }]
    )
  })

  test('normalizes order and scope', () => {
    assert.deepEqual(
      normalizeUpstreamProtocols(['responses', 'chat', 'x', 'chat']),
      ['chat', 'responses']
    )
    assert.equal(supportsProtocolRouting(1), true)
    assert.equal(supportsProtocolRouting(8), false)
  })
})
