import assert from 'node:assert/strict'
import { test } from 'node:test'
import { getErrorPageStatus, getHttpStatus } from './error-status'

test('只有带 HTTP 状态码的错误才返回状态码', () => {
  assert.equal(getHttpStatus({ response: { status: 429 } }), 429)
  assert.equal(getHttpStatus({ response: { status: 502 } }), 502)
  assert.equal(getHttpStatus(new Error('render failed')), undefined)
  assert.equal(getHttpStatus({ response: { status: '500' } }), undefined)
  assert.equal(getHttpStatus({ response: null }), undefined)
  assert.equal(getHttpStatus(undefined), undefined)
  assert.equal(getHttpStatus('boom'), undefined)
})

test('错误页：显式 500 页面仍显示 500，没有状态码的运行时错误不显示', () => {
  assert.equal(getErrorPageStatus(undefined), 500)
  assert.equal(getErrorPageStatus(new Error('render failed')), undefined)
  assert.equal(getErrorPageStatus({ response: { status: 429 } }), 429)
})
