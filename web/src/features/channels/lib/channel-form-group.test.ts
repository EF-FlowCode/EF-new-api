import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  transformFormDataToUpdatePayload,
} from './channel-form'

test('serializes a multi-group channel beyond the legacy 64 character limit', () => {
  const groups = [
    '1_vip_claude_max_nopic',
    'work_claude_max',
    '1_vip_claude_API2',
    'test_cc_max',
  ]
  const formData = {
    ...CHANNEL_FORM_DEFAULT_VALUES,
    name: 'test',
    models: 'claude-test',
    group: groups,
  }

  assert.equal(channelFormSchema.safeParse(formData).success, true)
  const payload = transformFormDataToUpdatePayload(formData, 1)
  assert.equal(payload.group, groups.join(','))
  assert.equal((payload.group?.length ?? 0) > 64, true)
})
