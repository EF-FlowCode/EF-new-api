import { AxiosError, AxiosHeaders } from 'axios'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { serverErrorMessage } from './handle-server-error'

function httpError(
  config: Record<string, unknown>,
  message = 'Quota exceeded'
) {
  const headers = new AxiosHeaders()
  const requestConfig = { headers, ...config }
  return new AxiosError(
    'Request failed with status code 400',
    'ERR_BAD_REQUEST',
    requestConfig,
    null,
    {
      data: { message },
      status: 400,
      statusText: 'Bad Request',
      headers,
      config: requestConfig,
    }
  )
}

test('mutation errors already shown by the interceptor are not repeated', () => {
  assert.equal(serverErrorMessage(httpError({})), null)
})

test('caller-handled HTTP errors use the backend message', () => {
  assert.equal(
    serverErrorMessage(httpError({ skipErrorNotification: true })),
    'Quota exceeded'
  )
})

test('plain errors keep their message', () => {
  assert.equal(
    serverErrorMessage(new Error('Invalid amount')),
    'Invalid amount'
  )
})
