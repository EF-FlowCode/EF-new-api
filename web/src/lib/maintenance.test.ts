import { AxiosError, type AxiosResponse } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import {
  getMaintenance,
  isMaintenanceError,
  probeMaintenance,
  setMaintenance,
} from './maintenance'

const originalFetch = globalThis.fetch
afterEach(() => {
  globalThis.fetch = originalFetch
  setMaintenance(false)
})

test('only explicit 503 maintenance errors activate the notice', () => {
  const response = {
    status: 503,
    data: { error: { code: 'maintenance' } },
  } as AxiosResponse
  assert.equal(
    isMaintenanceError(
      new AxiosError('maintenance', '', undefined, undefined, response)
    ),
    true
  )
  response.data.error.code = 'upstream_error'
  assert.equal(
    isMaintenanceError(
      new AxiosError('failed', '', undefined, undefined, response)
    ),
    false
  )
  assert.equal(isMaintenanceError(new Error('network unavailable')), false)
})

test('network failures and invalid responses cannot claim recovery', async () => {
  setMaintenance(true)
  globalThis.fetch = (() =>
    Promise.resolve(new Response('{}', { status: 200 }))) as typeof fetch
  await probeMaintenance(new AbortController().signal)
  assert.equal(getMaintenance(), true)
  globalThis.fetch = (() =>
    Promise.resolve(new Response('', { status: 502 }))) as typeof fetch
  await probeMaintenance(new AbortController().signal)
  assert.equal(getMaintenance(), true)
})

test('the only automatic request is a no-store status GET', async () => {
  setMaintenance(true)
  const seen: unknown[] = []
  globalThis.fetch = ((url, options) => {
    seen.push([url, options])
    return Promise.resolve(
      new Response('{"maintenance":false}', {
        headers: { 'Content-Type': 'application/json' },
      })
    )
  }) as typeof fetch
  const signal = new AbortController().signal
  await probeMaintenance(signal)
  assert.equal(getMaintenance(), false)
  assert.deepEqual(seen, [
    ['/__maintenance/status', { cache: 'no-store', signal }],
  ])
})
