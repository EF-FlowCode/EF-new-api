import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  DEFAULT_CHANNEL_COLUMN_VISIBILITY,
  parseChannelColumnVisibility,
} from './channel-column-visibility'

describe('channel column visibility', () => {
  test('shows ID, priority, and weight by default', () => {
    const visibility = parseChannelColumnVisibility(null)

    assert.equal(visibility.id, true)
    assert.equal(visibility.priority, true)
    assert.equal(visibility.weight, true)
    assert.equal(visibility.models, false)
  })

  test('preserves saved choices and ignores unknown columns', () => {
    const visibility = parseChannelColumnVisibility(
      JSON.stringify({ id: false, priority: false, unknown: true })
    )

    assert.equal(visibility.id, false)
    assert.equal(visibility.priority, false)
    assert.equal(visibility.weight, DEFAULT_CHANNEL_COLUMN_VISIBILITY.weight)
    assert.equal(visibility.unknown, undefined)
  })

  test('falls back to defaults for invalid JSON', () => {
    assert.deepEqual(
      parseChannelColumnVisibility('{invalid-json'),
      DEFAULT_CHANNEL_COLUMN_VISIBILITY
    )
  })

  test('fills newly introduced columns with their defaults', () => {
    const visibility = parseChannelColumnVisibility(
      JSON.stringify({ id: false })
    )

    assert.deepEqual(visibility, {
      ...DEFAULT_CHANNEL_COLUMN_VISIBILITY,
      id: false,
    })
  })
})
