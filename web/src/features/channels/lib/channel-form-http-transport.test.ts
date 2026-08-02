import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  buildSettingJSON,
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
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
