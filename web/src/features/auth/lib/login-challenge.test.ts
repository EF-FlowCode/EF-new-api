import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { challengeMethods, isLoginChallenge } from './login-challenge'

describe('login challenge', () => {
  test('recognizes both the new and the legacy challenge shape', () => {
    assert.equal(isLoginChallenge({ require_verification: true }), true)
    assert.equal(isLoginChallenge({ require_2fa: true }), true)
    assert.equal(isLoginChallenge({ id: 7 }), false)
    assert.equal(isLoginChallenge(undefined), false)
  })

  test('offers the methods the server listed, TOTP when it listed none', () => {
    assert.deepEqual(challengeMethods(null), { twoFA: true, passkey: false })
    assert.deepEqual(
      challengeMethods({
        methods: [
          { method: '2fa', available: false },
          { method: 'passkey', available: true },
        ],
      }),
      { twoFA: false, passkey: true }
    )
  })
})
