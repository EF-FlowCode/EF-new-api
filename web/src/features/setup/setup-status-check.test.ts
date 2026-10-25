import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { createSetupStatusCheck } from './setup-status-check'

describe('createSetupStatusCheck', () => {
  test('marks a successful initialized response as checked for this session', () => {
    const check = createSetupStatusCheck()

    assert.equal(check.shouldCheck('/dashboard'), true)
    assert.equal(
      check.consume({ success: true, data: { status: true } }),
      'continue'
    )
    assert.equal(check.shouldCheck('/dashboard'), false)
  })

  test('redirects an uninitialized response without marking it as checked', () => {
    const check = createSetupStatusCheck()

    assert.equal(
      check.consume({ success: true, data: { status: false } }),
      'redirect'
    )
    assert.equal(check.shouldCheck('/dashboard'), true)
  })

  test('retries after a failed or invalid response', () => {
    const check = createSetupStatusCheck()

    assert.equal(check.consume(null), 'retry')
    assert.equal(check.shouldCheck('/dashboard'), true)
    assert.equal(check.consume({ success: false }), 'retry')
    assert.equal(check.shouldCheck('/dashboard'), true)
    assert.equal(check.consume({ success: true }), 'retry')
    assert.equal(check.shouldCheck('/dashboard'), true)
    assert.equal(check.consume({ success: true, data: {} }), 'retry')
    assert.equal(check.shouldCheck('/dashboard'), true)
    assert.equal(
      check.consume({
        success: true,
        data: { status: 'invalid' as unknown as boolean },
      }),
      'retry'
    )
    assert.equal(check.shouldCheck('/dashboard'), true)
  })

  test('does not check while navigating within setup', () => {
    const check = createSetupStatusCheck()

    assert.equal(check.shouldCheck('/setup'), false)
    assert.equal(check.shouldCheck('/setup/'), false)
  })

  test('a new page-session instance checks setup status again', () => {
    const firstSession = createSetupStatusCheck()
    firstSession.consume({ success: true, data: { status: true } })

    const refreshedSession = createSetupStatusCheck()
    assert.equal(firstSession.shouldCheck('/dashboard'), false)
    assert.equal(refreshedSession.shouldCheck('/dashboard'), true)
  })
})
