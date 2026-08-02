import assert from 'node:assert/strict'
import { test } from 'node:test'
import { formatJsonDraft, isValidJsonDraft } from './json-code-editor'

test('validates and formats JSON drafts without changing invalid input', () => {
  assert.equal(isValidJsonDraft(''), true)
  assert.equal(isValidJsonDraft('{"ok":true}'), true)
  assert.equal(isValidJsonDraft('{broken'), false)
  assert.equal(formatJsonDraft('{"ok":true}'), '{\n  "ok": true\n}')
  assert.equal(formatJsonDraft('{broken'), null)
})
