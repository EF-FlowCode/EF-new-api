import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  ADVANCED_CUSTOM_BALANCE_PATH,
  ADVANCED_CUSTOM_MODEL_LIST_PATH,
  createAdvancedCustomEditorState,
  hasAdvancedCustomModelListRoute,
  parseAdvancedCustomConfig,
  validateAdvancedCustomConfig,
  type AdvancedCustomConfig,
} from './advanced-custom'

function route(overrides: Record<string, unknown> = {}) {
  return {
    incoming_path: '/v1/chat/completions',
    upstream_path: '/v1/chat/completions',
    converter: 'none',
    ...overrides,
  }
}

function config(...routes: Record<string, unknown>[]): AdvancedCustomConfig {
  return { advanced_routes: routes as AdvancedCustomConfig['advanced_routes'] }
}

function assertInvalid(value: AdvancedCustomConfig, message: string) {
  const error = validateAdvancedCustomConfig(value)
  assert.ok(error, `expected invalid config: ${message}`)
  assert.match(error, new RegExp(message, 'i'))
}

describe('advanced custom configuration validation', () => {
  test('builds normalized editor state from management routes', () => {
    const state = createAdvancedCustomEditorState(
      JSON.stringify(
        config(
          route({
            incoming_path: ` ${ADVANCED_CUSTOM_MODEL_LIST_PATH} `,
            upstream_path: ' /provider/models ',
          }),
          route({
            incoming_path: ADVANCED_CUSTOM_BALANCE_PATH,
            upstream_path: '/provider/balance',
          })
        )
      )
    )

    assert.equal(state.modelListPath, '/provider/models')
    assert.equal(state.balancePath, '/provider/balance')
    assert.deepEqual(JSON.parse(state.jsonText), {
      advanced_routes: [
        {
          incoming_path: ADVANCED_CUSTOM_MODEL_LIST_PATH,
          upstream_path: '/provider/models',
          converter: 'none',
          models: [],
        },
        {
          incoming_path: ADVANCED_CUSTOM_BALANCE_PATH,
          upstream_path: '/provider/balance',
          converter: 'none',
          models: [],
        },
      ],
    })
  })

  test('falls back to an empty editor state for malformed JSON', () => {
    const state = createAdvancedCustomEditorState('{invalid')
    assert.equal(state.modelListPath, '')
    assert.equal(state.balancePath, '')
    assert.deepEqual(JSON.parse(state.jsonText), { advanced_routes: [] })
  })

  test('detects model discovery routes without enabling legacy Custom', () => {
    const value = JSON.stringify(
      config({
        incoming_path: ADVANCED_CUSTOM_MODEL_LIST_PATH,
        upstream_path: '/models',
      })
    )
    assert.equal(hasAdvancedCustomModelListRoute(value), true)
    assert.equal(
      hasAdvancedCustomModelListRoute(
        JSON.stringify({ advanced_custom: JSON.parse(value) })
      ),
      true
    )
    assert.equal(
      hasAdvancedCustomModelListRoute('{"advanced_routes":[]}'),
      false
    )
    assert.equal(
      hasAdvancedCustomModelListRoute('{"not_advanced":true}'),
      false
    )
  })

  test('accepts relative and absolute upstream targets', () => {
    assert.equal(
      validateAdvancedCustomConfig(
        config(
          route(),
          route({
            incoming_path: '/v1/responses',
            upstream_path: 'HTTPS://provider.example/api?x=1#fragment',
          })
        )
      ),
      null
    )
  })

  test('requires at least one route', () => {
    assertInvalid({ advanced_routes: [] }, 'at least one route')
  })

  test('rejects query strings in incoming paths and unsafe upstream URLs', () => {
    assertInvalid(
      config(route({ incoming_path: '/v1/chat?stream=1' })),
      'query'
    )
    assertInvalid(
      config(route({ upstream_path: '//provider.example/path' })),
      'upstream_path'
    )
    assertInvalid(config(route({ upstream_path: 'http://' })), 'upstream_path')
    assertInvalid(
      config(route({ upstream_path: 'ftp://provider.example/path' })),
      'upstream_path'
    )
    assertInvalid(
      config(route({ upstream_path: 'provider.example/path' })),
      'upstream_path'
    )
  })

  test('enforces converter and incoming path compatibility', () => {
    assertInvalid(
      config(
        route({
          converter: 'openai_responses_to_openai_chat_completions',
        })
      ),
      'converter does not match'
    )
    assertInvalid(
      config(
        route({
          incoming_path: '/v1/alpha/search',
          converter: 'openai_chat_completions_to_openai_responses',
        })
      ),
      'converter does not match'
    )
    assert.equal(
      validateAdvancedCustomConfig(
        config(
          route({
            incoming_path: '/v1beta/models/{model}:streamGenerateContent',
            upstream_path: '/v1beta/models/{model}:generateContent',
            converter: 'gemini_generate_content_to_openai_chat_completions',
          })
        )
      ),
      null
    )
  })

  test('keeps management route constraints and validates auth before skipping them', () => {
    assertInvalid(
      config(
        route({
          incoming_path: ADVANCED_CUSTOM_MODEL_LIST_PATH,
          upstream_path: '/models/{model}',
          auth: { type: 'header' },
        })
      ),
      'management route cannot use'
    )
    assertInvalid(
      config(
        route({
          incoming_path: ADVANCED_CUSTOM_BALANCE_PATH,
          auth: { type: 'header', name: 'Authorization' },
        })
      ),
      'auth.value'
    )
    assertInvalid(
      config(
        route({
          incoming_path: ADVANCED_CUSTOM_MODEL_LIST_PATH,
          auth: { type: 'bearer' },
        })
      ),
      'auth type'
    )
  })

  test('enforces model uniqueness and catch-all ordering', () => {
    assertInvalid(
      config(route({ models: ['gpt-4o'] }), route({ models: ['gpt-4o'] })),
      'duplicate model'
    )
    assertInvalid(
      config(route({ models: [] }), route({ models: ['gpt-4o'] })),
      'catch-all route must be last'
    )
    assertInvalid(config(route({ models: ['re:'] })), 'invalid model regex')
    assertInvalid(config(route({ models: ['re:['] })), 'invalid model regex')
    assert.equal(
      validateAdvancedCustomConfig(config(route({ models: ['re:(?i)^gpt-'] }))),
      null
    )
  })

  test('rejects malformed JSON shapes before normalization can hide them', () => {
    assert.equal(
      parseAdvancedCustomConfig(
        JSON.stringify({
          advanced_routes: [route({ models: [{ bad: true }] })],
        })
      ),
      null
    )
    assert.equal(
      parseAdvancedCustomConfig(
        JSON.stringify({
          advanced_routes: [route({ auth: 'Bearer token' })],
        })
      ),
      null
    )
  })
})

describe('advanced custom route body pass-through', () => {
  test('keeps the flag only when enabled', () => {
    const parsed = parseAdvancedCustomConfig(
      JSON.stringify(
        config(
          route({ pass_through_body_enabled: true }),
          route({
            incoming_path: '/v1/messages',
            upstream_path: '/v1/messages',
            pass_through_body_enabled: false,
          })
        )
      )
    )
    assert.ok(parsed)
    assert.equal(parsed.advanced_routes[0].pass_through_body_enabled, true)
    assert.equal(
      'pass_through_body_enabled' in parsed.advanced_routes[1],
      false
    )
    assert.equal(validateAdvancedCustomConfig(parsed), null)
  })

  test('rejects pass-through on converter and management routes', () => {
    assertInvalid(
      config(
        route({
          incoming_path: '/v1/messages',
          upstream_path: '/v1/chat/completions',
          converter: 'anthropic_messages_to_openai_chat_completions',
          pass_through_body_enabled: true,
        })
      ),
      'requires native forwarding'
    )
    assertInvalid(
      config(
        route(),
        route({
          incoming_path: ADVANCED_CUSTOM_MODEL_LIST_PATH,
          upstream_path: '/models',
          pass_through_body_enabled: true,
        })
      ),
      'management route cannot pass through'
    )
  })
})
