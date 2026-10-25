import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { AuditLog } from '../api'
import {
  auditActionLabel,
  auditCategoryLabel,
  loginMethodLabel,
} from './audit-labels'

const t = (key: string, options?: Record<string, unknown>) =>
  key.replace(/\{\{(\w+)\}\}/g, (_, name) => String(options?.[name] ?? ''))

function log(overrides: Partial<AuditLog>): AuditLog {
  return {
    id: 1,
    event_id: 'e1',
    user_id: 7,
    username: 'alice',
    actor_role: 1,
    created_at: 1,
    category: 'operation',
    action: 'generic',
    token_ref: '',
    ip: '',
    user_agent: '',
    method: 'POST',
    route: '/api/user/manage',
    status: 200,
    success: true,
    request_id: '',
    content: 'POST /api/user/manage',
    other: null,
    ...overrides,
  }
}

describe('audit labels', () => {
  test('login methods include the two-factor suffix and OAuth provider', () => {
    assert.equal(loginMethodLabel('telegram+2fa', t), 'Telegram + two-factor')
    assert.equal(loginMethodLabel('oauth:github', t), 'github')
    assert.equal(loginMethodLabel('password', t), 'Password')
  })

  test('actions prefer the structured op, then known ids, then content', () => {
    assert.equal(
      auditActionLabel(
        log({ action: 'user.login', other: { login_method: 'passkey' } }),
        t
      ),
      'Signed in with Passkey'
    )
    assert.equal(
      auditActionLabel(log({ action: 'generic' }), t),
      'POST /api/user/manage'
    )
    assert.equal(
      auditActionLabel(
        log({ action: 'x', other: { op: { action: 'channel.key_view' } } }),
        t
      ),
      'Viewed a channel key'
    )
    assert.equal(
      auditActionLabel(log({ action: 'unknown.id', content: 'fallback' }), t),
      'fallback'
    )
  })

  test('redemption batch deletion has its own label', () => {
    assert.equal(
      auditActionLabel(log({ action: 'redemption.delete_batch' }), t),
      'Batch deleted redemption codes'
    )
  })

  test('unknown categories are shown verbatim', () => {
    assert.equal(auditCategoryLabel('login', t), 'Sign-in')
    assert.equal(auditCategoryLabel('other', t), 'other')
  })
})
