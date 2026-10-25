import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { getReasoningEffortVariant } from './format'

describe('reasoning effort badge variants', () => {
  test('maps all supported effort levels', () => {
    assert.equal(getReasoningEffortVariant('max'), 'orange')
    assert.equal(getReasoningEffortVariant('xhigh'), 'orange')
    assert.equal(getReasoningEffortVariant('high'), 'orange')
    assert.equal(getReasoningEffortVariant('medium'), 'yellow')
    assert.equal(getReasoningEffortVariant('low'), 'green')
    assert.equal(getReasoningEffortVariant('minimal'), 'green')
    assert.equal(getReasoningEffortVariant('none'), 'grey')
  })

  test('normalizes case and keeps unknown values neutral', () => {
    assert.equal(getReasoningEffortVariant(' HIGH '), 'orange')
    assert.equal(getReasoningEffortVariant('future-level'), 'grey')
    assert.equal(getReasoningEffortVariant(undefined), 'grey')
  })
})
