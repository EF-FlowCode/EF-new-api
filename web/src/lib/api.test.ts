import axios, { AxiosError, AxiosHeaders, type AxiosAdapter } from 'axios'
import i18next from 'i18next'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import zh from '../i18n/locales/zh.json'
import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  getRequestErrorMessage,
  installApiErrorInterceptor,
  type ApiRequestConfig,
  type ApiResult,
  unwrapApiResult,
} from './api'

test('GET requests with AbortSignal are never deduplicated', async () => {
  let adapterCalls = 0
  const adapter: AxiosAdapter = async (config) => {
    adapterCalls += 1
    return {
      data: { call: adapterCalls },
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      config,
    }
  }

  const first = new AbortController()
  const second = new AbortController()
  const [firstResponse, secondResponse] = await Promise.all([
    api.get('/abort-owned-dedup-test', { adapter, signal: first.signal }),
    api.get('/abort-owned-dedup-test', { adapter, signal: second.signal }),
  ])

  assert.equal(adapterCalls, 2)
  assert.notEqual(firstResponse.data.call, secondResponse.data.call)
})

function resultAdapter<T>(data: T): AxiosAdapter {
  return async (config) => ({
    data,
    status: 200,
    statusText: 'OK',
    headers: new AxiosHeaders(),
    config,
  })
}

function errorAdapter(status: number, message: string): AxiosAdapter {
  return async (config) => {
    throw new AxiosError(
      'Transport failed',
      AxiosError.ERR_BAD_RESPONSE,
      config,
      undefined,
      {
        data: { message },
        status,
        statusText: 'Request failed',
        headers: new AxiosHeaders(),
        config,
      }
    )
  }
}

test('dependency failures use the available translation and notify once', async () => {
  const language = i18next.language
  const locale = 'zh'
  await i18next.init({ lng: locale, resources: { [locale]: zh } })
  try {
    const notifications: string[] = []
    const client = axios.create()
    installApiErrorInterceptor(client, {
      notifyError: (message) => notifications.push(message),
    })
    const message = 'verification store unavailable; please retry later'
    await assert.rejects(
      client.get('/dependency-failure', { adapter: errorAdapter(503, message) })
    )
    assert.deepEqual(notifications, [zh.translation[message]])
  } finally {
    i18next.removeResourceBundle(locale, 'translation')
    await i18next.changeLanguage(language || 'en')
  }
})

test('default business failure is notified once', async () => {
  const notifications: string[] = []
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => undefined,
  })

  const response = await client.get<ApiResult<never>>('/business-failure', {
    adapter: resultAdapter({
      success: false,
      message: 'Business failed',
    }),
  })

  assert.equal(response.data.success, false)
  assert.deepEqual(notifications, ['Business failed'])
})

test('default HTTP failure is notified once and remains rejected', async () => {
  const notifications: string[] = []
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => undefined,
  })

  await assert.rejects(
    client.get('/http-failure', {
      adapter: errorAdapter(500, 'Server failed'),
    }),
    /Transport failed/
  )

  assert.deepEqual(notifications, ['Server failed'])
})

test('caller-handled business failure bypasses the global notification', async () => {
  const notifications: string[] = []
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => undefined,
  })

  const response = await client.get<ApiResult<never>>('/business-failure', {
    ...CALLER_HANDLED_REQUEST_CONFIG,
    adapter: resultAdapter({
      success: false,
      message: 'Business failed',
    }),
  })

  assert.equal(response.data.success, false)
  assert.deepEqual(notifications, [])
})

test('caller-handled HTTP failure bypasses the global notification', async () => {
  const notifications: string[] = []
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => undefined,
  })

  await assert.rejects(
    client.get('/http-failure', {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      adapter: errorAdapter(500, 'Server failed'),
    }),
    /Transport failed/
  )

  assert.deepEqual(notifications, [])
})

test('caller-handled 401 resets auth without a global notification', async () => {
  const notifications: string[] = []
  let authResetCount = 0
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => {
      authResetCount += 1
    },
  })

  await assert.rejects(
    client.get('/unauthorized', {
      ...CALLER_HANDLED_REQUEST_CONFIG,
      adapter: errorAdapter(401, 'Unauthorized'),
    })
  )

  assert.deepEqual(notifications, [])
  assert.equal(authResetCount, 1)
})

test('skipBusinessError only bypasses business failures', async () => {
  const notifications: string[] = []
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => undefined,
  })

  await client.get<ApiResult<never>>('/business-failure', {
    ...({ skipBusinessError: true } satisfies ApiRequestConfig),
    adapter: resultAdapter({
      success: false,
      message: 'Business failed',
    }),
  })
  await assert.rejects(
    client.get('/http-failure', {
      ...({ skipBusinessError: true } satisfies ApiRequestConfig),
      adapter: errorAdapter(500, 'Server failed'),
    })
  )

  assert.deepEqual(notifications, ['Server failed'])
})

test('skipErrorHandler only bypasses HTTP failures', async () => {
  const notifications: string[] = []
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => undefined,
  })

  await client.get<ApiResult<never>>('/business-failure', {
    ...({ skipErrorHandler: true } satisfies ApiRequestConfig),
    adapter: resultAdapter({
      success: false,
      message: 'Business failed',
    }),
  })
  await assert.rejects(
    client.get('/http-failure', {
      ...({ skipErrorHandler: true } satisfies ApiRequestConfig),
      adapter: errorAdapter(500, 'Server failed'),
    })
  )

  assert.deepEqual(notifications, ['Business failed'])
})

test('skipErrorHandler still bypasses all global 401 handling', async () => {
  const notifications: string[] = []
  let authResetCount = 0
  const client = axios.create()
  installApiErrorInterceptor(client, {
    notifyError: (message) => notifications.push(message),
    resetAuth: () => {
      authResetCount += 1
    },
  })

  await assert.rejects(
    client.get('/unauthorized', {
      ...({ skipErrorHandler: true } satisfies ApiRequestConfig),
      adapter: errorAdapter(401, 'Unauthorized'),
    })
  )

  assert.deepEqual(notifications, [])
  assert.equal(authResetCount, 0)
})

test('unwrapApiResult returns successful response data', () => {
  const data = { id: 42 }

  assert.equal(
    unwrapApiResult({ success: true, data }, 'Fallback message'),
    data
  )
})

test('unwrapApiResult throws the business message with a fallback', () => {
  assert.throws(
    () =>
      unwrapApiResult(
        { success: false, message: 'Business failed' },
        'Fallback message'
      ),
    /Business failed/
  )
  assert.throws(
    () => unwrapApiResult({ success: false }, 'Fallback message'),
    /Fallback message/
  )
})

test('getRequestErrorMessage prefers the server response message', async () => {
  const client = axios.create()
  let requestError: unknown

  try {
    await client.get('/http-failure', {
      adapter: errorAdapter(500, 'Server failed'),
    })
  } catch (error) {
    requestError = error
  }

  assert.equal(
    getRequestErrorMessage(requestError, 'Fallback message'),
    'Server failed'
  )
})

test('getRequestErrorMessage supports ordinary errors and unknown values', () => {
  assert.equal(
    getRequestErrorMessage(new Error('Local failure'), 'Fallback message'),
    'Local failure'
  )
  assert.equal(
    getRequestErrorMessage(null, 'Fallback message'),
    'Fallback message'
  )
})
