import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  parseConditionValue,
  parseLooseValue,
} from './param-override-condition'

test('keeps regex condition values as literal strings', () => {
  assert.equal(
    parseConditionValue({ mode: 'regex', value_text: '^gpt-' }),
    '^gpt-'
  )
  // Values that look like JSON stay strings so the backend receives a pattern.
  assert.equal(parseConditionValue({ mode: 'regex', value_text: '123' }), '123')
  assert.equal(
    parseConditionValue({ mode: 'regex', value_text: '"quoted"' }),
    '"quoted"'
  )
  // Surrounding whitespace is part of the pattern.
  assert.equal(parseConditionValue({ mode: 'regex', value_text: ' a ' }), ' a ')
  assert.equal(parseConditionValue({ mode: 'regex', value_text: '' }), '')
})

test('keeps loose JSON parsing for non-regex conditions', () => {
  assert.equal(parseConditionValue({ mode: 'full', value_text: '123' }), 123)
  assert.equal(parseConditionValue({ mode: 'full', value_text: 'true' }), true)
  assert.equal(
    parseConditionValue({ mode: 'contains', value_text: ' gpt ' }),
    'gpt'
  )
  assert.equal(parseLooseValue(''), '')
})
