import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { BUILTIN_BINDING_TYPE_BY_FIELD } from './builtin-binding-types'

describe('BUILTIN_BINDING_TYPE_BY_FIELD', () => {
  test('maps user fields to the binding types accepted by the admin API', () => {
    assert.deepEqual(BUILTIN_BINDING_TYPE_BY_FIELD, {
      email: 'email',
      github_id: 'github',
      discord_id: 'discord',
      wechat_id: 'wechat',
      oidc_id: 'oidc',
      telegram_id: 'telegram',
      linux_do_id: 'linuxdo',
    })
  })
})
