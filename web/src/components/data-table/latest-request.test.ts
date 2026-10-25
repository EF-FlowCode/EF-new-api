import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { createLatestRequestState } from './latest-request'

describe('latest request state', () => {
  it('aborts the previous request when a newer request starts', () => {
    const state = createLatestRequestState()
    const first = state.begin()
    const second = state.begin()

    assert.equal(first.signal.aborted, true)
    assert.equal(second.signal.aborted, false)
  })

  it('does not let an older response commit after a newer request starts', () => {
    const state = createLatestRequestState()
    const first = state.begin()
    state.begin()
    let commits = 0

    assert.equal(
      first.commit(() => commits++),
      false
    )
    assert.equal(commits, 0)
  })

  it('lets the current response commit', () => {
    const state = createLatestRequestState()
    const current = state.begin()
    let commits = 0

    assert.equal(
      current.commit(() => commits++),
      true
    )
    assert.equal(commits, 1)
  })

  it('aborts and invalidates the current request when disposed', () => {
    const state = createLatestRequestState()
    const current = state.begin()
    let commits = 0

    state.dispose()

    assert.equal(current.signal.aborted, true)
    assert.equal(
      current.commit(() => commits++),
      false
    )
    assert.equal(commits, 0)
    assert.throws(() => state.begin(), /after disposal/)
  })
})
