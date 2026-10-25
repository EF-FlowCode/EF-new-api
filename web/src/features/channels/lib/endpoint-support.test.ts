import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  getAdvancedCustomConverterUpstreamPath,
  getAdvancedCustomEndpointType,
} from './advanced-custom'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformFormDataToCreatePayload,
} from './channel-form'
import {
  getChannelEndpointSupport,
  supportsEndpointOverride,
} from './endpoint-support'

function routes(...incoming: string[]): string {
  return JSON.stringify({
    advanced_routes: incoming.map((path) => ({
      incoming_path: path,
      upstream_path: path,
      converter: 'none',
    })),
  })
}

describe('channel endpoint support', () => {
  test('derives restricted ordinary types from the capability table', () => {
    assert.deepEqual(getChannelEndpointSupport(14, ''), {
      source: 'type',
      endpoints: ['openai', 'anthropic'],
    })
    assert.deepEqual(getChannelEndpointSupport(1, ''), {
      source: 'type',
      endpoints: null,
    })
  })

  test('derives Custom endpoints from relay routes only', () => {
    assert.deepEqual(
      getChannelEndpointSupport(
        8,
        routes('/v1/chat/completions', '/v1/responses', '/v1/models')
      ),
      { source: 'routes', endpoints: ['openai', 'openai-response'] }
    )
    assert.deepEqual(getChannelEndpointSupport(8, routes('/v1/models')), {
      source: 'legacy-custom',
      endpoints: null,
    })
    assert.equal(getChannelEndpointSupport(62, '').source, 'preset')
  })

  test('only ordinary channels honor the manual override', () => {
    assert.equal(supportsEndpointOverride(1), true)
    assert.equal(supportsEndpointOverride(14), true)
    assert.equal(supportsEndpointOverride(8), false)
    assert.equal(supportsEndpointOverride(63), false)
  })

  test('maps incoming paths and converter upstream defaults', () => {
    assert.equal(
      getAdvancedCustomEndpointType('/v1beta/models/{model}:generateContent'),
      'gemini'
    )
    assert.equal(getAdvancedCustomEndpointType('/v1/completions'), null)
    assert.equal(
      getAdvancedCustomConverterUpstreamPath(
        'openai_responses_to_openai_chat_completions',
        '/v1/responses'
      ),
      '/v1/chat/completions'
    )
    assert.equal(
      getAdvancedCustomConverterUpstreamPath('none', '/v1/messages'),
      '/v1/messages'
    )
  })
})

describe('manual endpoint override persistence', () => {
  const values = (type: number) => ({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'c',
    models: 'm',
    key: 'k',
    type,
    supported_endpoint_types: ['openai-response'],
  })
  const saved = (type: number) =>
    JSON.parse(
      transformFormDataToCreatePayload(values(type)).channel.settings || '{}'
    ).supported_endpoint_types

  test('keeps the override on ordinary channels', () => {
    assert.deepEqual(saved(1), ['openai-response'])
  })

  test('drops the ignored override on Custom channels', () => {
    assert.equal(saved(8), undefined)
  })
})
