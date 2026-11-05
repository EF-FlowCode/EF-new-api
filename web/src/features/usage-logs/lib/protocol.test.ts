import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { inboundProtocol, protocolRoute } from './protocol'

describe('log protocol', () => {
  test('maps governed paths, including the playground prefix', () => {
    assert.equal(inboundProtocol('/v1/chat/completions'), 'Chat')
    assert.equal(inboundProtocol('/pg/chat/completions'), 'Chat')
    assert.equal(inboundProtocol('/v1/messages?beta=true'), 'Messages')
    assert.equal(inboundProtocol('/v1/responses/compact'), 'Responses')
    assert.equal(inboundProtocol('/v1/embeddings'), null)
    assert.equal(inboundProtocol(undefined), null)
  })

  test('the last protocol attempt decides the route', () => {
    assert.deepEqual(
      protocolRoute({
        admin_info: {
          protocol_attempts: [
            { channel_id: 1, mode: 'native', status: 400 },
            { channel_id: 1, mode: 'chat_fallback', status: 200 },
          ],
        },
        request_conversion: ['Claude Messages', 'OpenAI Compatible'],
      }),
      { kind: 'chat_fallback' }
    )
  })

  test('without attempts the conversion chain decides', () => {
    assert.deepEqual(
      protocolRoute({ request_conversion: ['OpenAI Responses'] }),
      {
        kind: 'native',
      }
    )
    assert.deepEqual(
      protocolRoute({
        request_conversion: ['OpenAI Compatible', 'Claude Messages'],
      }),
      { kind: 'converted', target: 'Claude Messages' }
    )
    assert.equal(protocolRoute({}), null)
    assert.equal(protocolRoute(null), null)
  })
})
