import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  getOptionInvalidationQueryKeys,
  isStatusRelatedKey,
  submitOptionsBulk,
} from './use-update-option'

test('bulk option submission sends every field in one request', async () => {
  const calls: unknown[] = []
  const options = [
    { key: 'Price', value: 1.5 },
    { key: 'MinTopUp', value: 10 },
  ]

  const response = await submitOptionsBulk(options, async (request) => {
    calls.push(request)
    return { success: true, message: '' }
  })

  assert.deepEqual(response, { success: true, message: '' })
  assert.deepEqual(calls, [{ options }])
})

test('bulk option invalidation de-duplicates shared caches', () => {
  assert.deepEqual(
    getOptionInvalidationQueryKeys([
      { key: 'HeaderNavModules', value: '[]' },
      { key: 'Notice', value: 'updated' },
      { key: 'ModelRatio', value: '{}' },
      { key: 'GroupRatio', value: '{}' },
    ]),
    [['system-options'], ['status'], ['pricing']]
  )
})

test('bulk option invalidation always refreshes system options', () => {
  assert.deepEqual(getOptionInvalidationQueryKeys([]), [['system-options']])
})

test('status-backed settings refresh the status cache', () => {
  for (const key of [
    'SystemName',
    'Logo',
    'Footer',
    'Chats',
    'general_setting.docs_link',
    'console_setting.announcements',
    'legal.user_agreement',
    'checkin_setting.enabled',
    'passkey.enabled',
    'oidc.enabled',
    'TurnstileCheckEnabled',
  ]) {
    assert.ok(isStatusRelatedKey(key), key)
  }
  assert.equal(isStatusRelatedKey('ModelRatio'), false)
  assert.equal(isStatusRelatedKey('SMTPServer'), false)
})
