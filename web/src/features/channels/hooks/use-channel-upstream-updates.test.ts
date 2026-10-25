import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { describe, it } from 'node:test'

describe('channel upstream update request ownership', () => {
  it('suppresses global business and HTTP error notifications for all four locally handled requests', () => {
    const source = readFileSync(
      new URL('./use-channel-upstream-updates.ts', import.meta.url),
      'utf8'
    )

    assert.match(
      source,
      /const UPSTREAM_UPDATE_REQUEST_CONFIG = \{\s*\.\.\.CALLER_HANDLED_REQUEST_CONFIG,\s*skipErrorHandler: true,\s*\}/
    )
    assert.equal(
      source.match(/UPSTREAM_UPDATE_REQUEST_CONFIG/g)?.length,
      5,
      'the shared config must be declared once and used by every request'
    )
  })
})
