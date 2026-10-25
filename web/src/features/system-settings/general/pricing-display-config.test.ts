import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  isObjectJson,
  parseEndpointOverrideConfig,
  parseEndpointOverrideRows,
  parseModelContextConfig,
  parseModelContextRows,
  serializeEndpointOverrideConfig,
  serializeEndpointOverrideRows,
  serializeModelContextConfig,
  serializeModelContextRows,
} from './pricing-display-config'

describe('pricing display config conversion', () => {
  test('accepts JSON objects for raw editing and rejects arrays or invalid text', () => {
    assert.equal(isObjectJson('{}'), true)
    assert.equal(isObjectJson('{"default":128000}'), true)
    assert.equal(isObjectJson('[]'), false)
    assert.equal(isObjectJson('{broken'), false)
  })

  test('reads full and shorthand endpoint entries', () => {
    assert.deepEqual(
      parseEndpointOverrideRows(
        '{"default":{"type":"openai","path":"/v1/chat/completions","method":"POST"},"1_vip_china":"/v1/chat/completions"}'
      ),
      [
        {
          group: 'default',
          type: 'openai',
          path: '/v1/chat/completions',
          method: 'POST',
        },
        {
          group: '1_vip_china',
          type: 'openai',
          path: '/v1/chat/completions',
          method: 'POST',
        },
      ]
    )
  })

  test('serializes endpoint rows without losing method or type', () => {
    const value = serializeEndpointOverrideRows([
      {
        group: '1_vip_china',
        type: 'openai',
        path: '/v1/chat/completions',
        method: 'POST',
      },
    ])

    assert.deepEqual(JSON.parse(value), {
      '1_vip_china': {
        type: 'openai',
        path: '/v1/chat/completions',
        method: 'POST',
      },
    })
  })

  test('round-trips model context rows and keeps default fallback', () => {
    const rows = parseModelContextRows(
      '{"default":128000,"doubao-seed-2.0-lite":262144}'
    )

    assert.deepEqual(rows, [
      { model: 'default', tokens: 128000 },
      { model: 'doubao-seed-2.0-lite', tokens: 262144 },
    ])
    assert.deepEqual(JSON.parse(serializeModelContextRows(rows)), {
      default: 128000,
      'doubao-seed-2.0-lite': 262144,
    })
  })

  test('separates endpoint fallback from group-specific rows', () => {
    const config = parseEndpointOverrideConfig(
      '{"default":{"type":"anthropic","path":"/v1/messages","method":"POST"},"vip":"/v1/chat/completions"}'
    )

    assert.deepEqual(config.fallback, {
      group: 'default',
      type: 'anthropic',
      path: '/v1/messages',
      method: 'POST',
    })
    assert.deepEqual(config.rows, [
      {
        group: 'vip',
        type: 'openai',
        path: '/v1/chat/completions',
        method: 'POST',
      },
    ])
    assert.deepEqual(
      JSON.parse(serializeEndpointOverrideConfig(config.fallback, config.rows)),
      {
        default: {
          type: 'anthropic',
          path: '/v1/messages',
          method: 'POST',
        },
        vip: {
          type: 'openai',
          path: '/v1/chat/completions',
          method: 'POST',
        },
      }
    )
  })

  test('uses the built-in endpoint fallback when JSON has no default', () => {
    assert.deepEqual(parseEndpointOverrideConfig('{}').fallback, {
      group: 'default',
      type: 'openai',
      path: '/v1/chat/completions',
      method: 'POST',
    })
  })

  test('separates optional context fallback from model-specific rows', () => {
    const config = parseModelContextConfig(
      '{"default":262144,"doubao-seed-2.0-lite":128000}'
    )

    assert.equal(config.fallbackTokens, 262144)
    assert.deepEqual(config.rows, [
      { model: 'doubao-seed-2.0-lite', tokens: 128000 },
    ])
    assert.deepEqual(
      JSON.parse(serializeModelContextConfig(null, config.rows)),
      { 'doubao-seed-2.0-lite': 128000 }
    )
  })
})
