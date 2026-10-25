import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Channel } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from './channel-form'

function awsSettings(
  overrides: Partial<typeof CHANNEL_FORM_DEFAULT_VALUES>
): Record<string, unknown> {
  return JSON.parse(
    transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'aws',
      models: 'claude-opus-4-7',
      key: 'key|us-east-1',
      type: 33,
      ...overrides,
    }).channel.settings as string
  )
}

function loadAWS(settings: Record<string, unknown>) {
  return transformChannelToFormDefaults({
    id: 1,
    type: 33,
    name: 'aws',
    key: '',
    models: 'claude-opus-4-7',
    group: 'default',
    status: 1,
    settings: JSON.stringify(settings),
    channel_info: { is_multi_key: false, multi_key_mode: 'random' },
  } as unknown as Channel)
}

test('AWS channels default to InvokeModel and can opt into Mantle', () => {
  assert.equal(awsSettings({}).aws_api_type, 'invoke_model')
  assert.equal(awsSettings({ aws_api_type: 'mantle' }).aws_api_type, 'mantle')
})

test('AWS interface settings are removed from other channel types', () => {
  const settings = awsSettings({
    type: 1,
    aws_api_type: 'mantle',
    settings: JSON.stringify({ aws_api_type: 'mantle', aws_key_type: 'ak_sk' }),
  })
  assert.equal(settings.aws_api_type, undefined)
  assert.equal(settings.aws_key_type, undefined)
})

test('loads the AWS interface from channel settings', () => {
  assert.equal(loadAWS({}).aws_api_type, 'invoke_model')
  assert.equal(loadAWS({ aws_api_type: 'mantle' }).aws_api_type, 'mantle')
  assert.equal(
    loadAWS({ aws_api_type: 'unknown' }).aws_api_type,
    'invoke_model'
  )
})
