import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { Channel } from '../types'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformChannelToFormDefaults,
  transformFormDataToCreatePayload,
} from './channel-form'

function vertexSettings(
  overrides: Partial<typeof CHANNEL_FORM_DEFAULT_VALUES>
): Record<string, unknown> {
  return JSON.parse(
    transformFormDataToCreatePayload({
      ...CHANNEL_FORM_DEFAULT_VALUES,
      name: 'vertex',
      models: 'gemini-2.5-pro',
      key: 'test-key',
      type: 41,
      ...overrides,
    }).channel.settings as string
  )
}

test('saves a trimmed Vertex project ID only in API Key mode', () => {
  const saved = vertexSettings({
    vertex_key_type: 'api_key',
    vertex_project_id: '  my-project  ',
  })
  assert.equal(saved.vertex_key_type, 'api_key')
  assert.equal(saved.vertex_project_id, 'my-project')

  const blank = vertexSettings({
    vertex_key_type: 'api_key',
    vertex_project_id: '   ',
    settings: JSON.stringify({ vertex_project_id: 'stale' }),
  })
  assert.equal(blank.vertex_project_id, undefined)

  const json = vertexSettings({
    vertex_key_type: 'json',
    vertex_project_id: 'my-project',
    settings: JSON.stringify({ vertex_project_id: 'stale' }),
  })
  assert.equal(json.vertex_project_id, undefined)

  const other = vertexSettings({
    type: 1,
    vertex_project_id: 'my-project',
    settings: JSON.stringify({
      vertex_key_type: 'api_key',
      vertex_project_id: 'stale',
    }),
  })
  assert.equal(other.vertex_key_type, undefined)
  assert.equal(other.vertex_project_id, undefined)
})

test('loads the Vertex project ID from channel settings', () => {
  const values = transformChannelToFormDefaults({
    id: 1,
    type: 41,
    name: 'vertex',
    key: '',
    models: 'gemini-2.5-pro',
    group: 'default',
    status: 1,
    settings: JSON.stringify({
      vertex_key_type: 'api_key',
      vertex_project_id: 'my-project',
    }),
    channel_info: { is_multi_key: false, multi_key_mode: 'random' },
  } as unknown as Channel)
  assert.equal(values.vertex_key_type, 'api_key')
  assert.equal(values.vertex_project_id, 'my-project')
})
