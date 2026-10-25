import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  transformFormDataToUpdatePayload,
} from './channel-form'

describe('transformFormDataToUpdatePayload status', () => {
  test('omits an unchanged status so a concurrent auto-disable survives', () => {
    const payload = transformFormDataToUpdatePayload(
      { ...CHANNEL_FORM_DEFAULT_VALUES, status: 1 },
      7,
      1
    )
    assert.equal('status' in payload, false)
  })

  test('sends a status the admin toggled', () => {
    const payload = transformFormDataToUpdatePayload(
      { ...CHANNEL_FORM_DEFAULT_VALUES, status: 2 },
      7,
      1
    )
    assert.equal(payload.status, 2)
  })
})
