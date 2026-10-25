import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { isSessionExpiredError } from './session-expiry'

describe('isSessionExpiredError', () => {
  test('only treats HTTP 401 as session expiry', () => {
    assert.equal(isSessionExpiredError({ response: { status: 401 } }), true)
    assert.equal(isSessionExpiredError({ response: { status: 403 } }), false)
    assert.equal(isSessionExpiredError({ response: { status: 500 } }), false)
    assert.equal(isSessionExpiredError(new Error('network error')), false)
  })
})
