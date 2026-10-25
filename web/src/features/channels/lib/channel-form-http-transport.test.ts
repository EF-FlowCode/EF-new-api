import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformFormDataToCreatePayload,
} from './channel-form'

test('validates and serializes channel HTTP transport settings', () => {
  const validForm = {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'test',
    models: 'gpt-4o',
  }

  assert.equal(
    channelFormSchema.safeParse({
      ...validForm,
      proxy: 'socks5h://127.0.0.1:1080',
    }).success,
    true
  )
  assert.equal(
    channelFormSchema.safeParse({
      ...validForm,
      proxy: 'socks5://127.0.0.1:1080/path',
    }).success,
    false
  )
  assert.equal(
    channelFormSchema.safeParse({
      ...validForm,
      http_protocol: 'http1',
      http2_connection_shards: 2,
    }).success,
    false
  )

  const defaults = JSON.parse(buildSettingJSON(CHANNEL_FORM_DEFAULT_VALUES))
  assert.equal(defaults.http_protocol, undefined)
  assert.equal(defaults.http2_connection_shards, undefined)

  const sharded = JSON.parse(
    buildSettingJSON({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      http2_connection_shards: 4,
    })
  )
  assert.equal(sharded.http2_connection_shards, 4)

  const http1 = JSON.parse(
    buildSettingJSON({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      proxy: ' https://proxy.example:8443 ',
      http_protocol: 'http1',
      http2_connection_shards: 8,
    })
  )
  assert.equal(http1.proxy, 'https://proxy.example:8443')
  assert.equal(http1.http_protocol, 'http1')
  assert.equal(http1.http2_connection_shards, undefined)
})

test('limits field passthrough controls to eflow gateway channel types', () => {
  const enabled = {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'gateway',
    models: 'gpt-4o',
    key: 'sk-test',
    allow_service_tier: true,
    disable_store: true,
    allow_safety_identifier: true,
    allow_include_obfuscation: true,
    allow_inference_geo: true,
    allow_speed: true,
    claude_beta_query: true,
  }

  for (const type of [1, 57]) {
    const settings = JSON.parse(
      transformFormDataToCreatePayload({ ...enabled, type }).channel
        .settings as string
    )
    assert.equal(settings.allow_service_tier, true)
    assert.equal(settings.disable_store, true)
    assert.equal(settings.allow_safety_identifier, true)
    assert.equal(settings.allow_include_obfuscation, true)
    assert.equal(settings.allow_inference_geo, true)
  }

  const claudeSettings = JSON.parse(
    transformFormDataToCreatePayload({ ...enabled, type: 14 }).channel
      .settings as string
  )
  assert.equal(claudeSettings.allow_service_tier, true)
  assert.equal(claudeSettings.allow_speed, true)
  assert.equal(claudeSettings.claude_beta_query, true)
  assert.equal(claudeSettings.disable_store, undefined)

  for (const type of [1001, 1002, 1003, 1004]) {
    const settings = JSON.parse(
      transformFormDataToCreatePayload({ ...enabled, type }).channel
        .settings as string
    )
    assert.equal(settings.allow_service_tier, undefined)
    assert.equal(settings.disable_store, undefined)
    assert.equal(settings.allow_safety_identifier, undefined)
    assert.equal(settings.allow_include_obfuscation, undefined)
    assert.equal(settings.allow_inference_geo, undefined)
    assert.equal(settings.allow_speed, undefined)
    assert.equal(settings.claude_beta_query, undefined)
  }
})

test('clears stale model-sync state when Advanced Custom has no model route', () => {
  const payload = transformFormDataToCreatePayload({
    ...CHANNEL_FORM_DEFAULT_VALUES,
    type: 8,
    name: 'custom',
    models: 'gpt-test',
    advanced_custom: JSON.stringify({
      advanced_routes: [
        {
          incoming_path: '/v1/chat/completions',
          upstream_path: '/chat',
          models: ['gpt-test'],
        },
      ],
    }),
    settings: JSON.stringify({
      upstream_model_update_check_enabled: true,
      upstream_model_update_auto_sync_enabled: true,
      upstream_model_update_last_check_time: 123,
      upstream_model_update_last_detected_models: ['gpt-test'],
      upstream_model_update_last_removed_models: ['old-model'],
      upstream_model_update_ignored_models: ['ignored-model'],
    }),
  })

  const settings = JSON.parse(payload.channel.settings as string)
  assert.equal(settings.advanced_custom.advanced_routes.length, 1)
  assert.equal(settings.upstream_model_update_check_enabled, undefined)
  assert.equal(settings.upstream_model_update_auto_sync_enabled, undefined)
  assert.equal(settings.upstream_model_update_last_check_time, undefined)
  assert.equal(settings.upstream_model_update_last_detected_models, undefined)
  assert.equal(settings.upstream_model_update_last_removed_models, undefined)
  assert.equal(settings.upstream_model_update_ignored_models, undefined)
})

test('serializes optional endpoint capabilities and clears them when empty', () => {
  const configured = JSON.parse(
    transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      supported_endpoint_types: ['openai-response', 'anthropic'],
    }).channel.settings as string
  )
  assert.deepEqual(configured.supported_endpoint_types, [
    'openai-response',
    'anthropic',
  ])

  const legacy = JSON.parse(
    transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      settings: JSON.stringify({
        supported_endpoint_types: ['openai-response'],
      }),
      supported_endpoint_types: [],
    }).channel.settings as string
  )
  assert.equal(legacy.supported_endpoint_types, undefined)
})
