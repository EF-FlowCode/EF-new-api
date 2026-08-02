import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { createChannelFieldUpdateScheduler } from './channel-field-update'

describe('createChannelFieldUpdateScheduler', () => {
  test('coalesces rapid changes and commits only the latest value', () => {
    const pending = new Map<number, () => void>()
    let nextId = 1
    const updates: number[] = []
    const scheduler = createChannelFieldUpdateScheduler(
      (value) => updates.push(value),
      {
        setTimeout(callback) {
          const id = nextId++
          pending.set(id, callback)
          return id
        },
        clearTimeout(id) {
          pending.delete(id)
        },
      }
    )

    scheduler.schedule(1)
    scheduler.schedule(2)
    scheduler.schedule(3)

    assert.equal(pending.size, 1)
    for (const callback of pending.values()) callback()
    assert.deepEqual(updates, [3])
  })
})
