import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  hasResponseModelDetails,
  isResponseModelMismatch,
  type ResponseModelObservation,
} from './response-model'

const obs = (
  returned_model: string,
  requested_model = 'requested',
  upstream_model = 'mapped'
): ResponseModelObservation => ({
  requested_model,
  upstream_model,
  returned_model,
})

describe('response model mismatch', () => {
  test('missing or empty observations never warn', () => {
    assert.equal(isResponseModelMismatch(undefined), false)
    assert.equal(isResponseModelMismatch(null), false)
    assert.equal(isResponseModelMismatch(obs('')), false)
    assert.equal(isResponseModelMismatch(obs('   ')), false)
  })

  test('requested or upstream names, case, dated versions and provider paths match', () => {
    for (const returned of [
      'requested',
      'mapped',
      'REQUESTED',
      'requested-2026-09-01',
      'Mapped-2026',
      'vendor/requested',
    ]) {
      assert.equal(isResponseModelMismatch(obs(returned)), false, returned)
    }
    assert.equal(
      isResponseModelMismatch(
        obs('deepseek/deepseek-v4.1-flash', 'deepseek-v4.1-flash', '')
      ),
      false
    )
  })

  test('different names warn', () => {
    assert.equal(isResponseModelMismatch(obs('other')), true)
    assert.equal(isResponseModelMismatch(obs('request')), true)
    assert.equal(isResponseModelMismatch(obs('vendor/other')), true)
  })

  test('empty expected names do not match every prefix', () => {
    assert.equal(isResponseModelMismatch(obs('other', '', '')), true)
    assert.equal(isResponseModelMismatch(obs('other', 'requested', '')), true)
  })

  test('a legacy stored flag is ignored in favour of the current rule', () => {
    const legacy = {
      ...obs('requested-2026-09-01'),
      mismatch: true,
    } as ResponseModelObservation
    assert.equal(isResponseModelMismatch(legacy), false)
  })
})

describe('response model details', () => {
  test('identical names need no details', () => {
    assert.equal(hasResponseModelDetails(undefined), false)
    assert.equal(
      hasResponseModelDetails(obs('requested', 'requested', '')),
      false
    )
    assert.equal(
      hasResponseModelDetails(obs('requested', 'requested', 'requested')),
      false
    )
  })

  test('mapping, compatible differences and mismatches show details', () => {
    assert.equal(hasResponseModelDetails(obs('requested')), true)
    assert.equal(
      hasResponseModelDetails(obs('REQUESTED', 'requested', '')),
      true
    )
    assert.equal(hasResponseModelDetails(obs('other', 'requested', '')), true)
  })
})
