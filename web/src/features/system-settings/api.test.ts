import { AxiosError, AxiosHeaders, type AxiosAdapter } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api } from '@/lib/api'
import {
  OptionSavedNotPublishedError,
  updateSystemOption,
  updateSystemOptionsBulk,
} from './api'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

function adapter(status: number, data: unknown): AxiosAdapter {
  return async (config) => {
    const response = {
      data,
      status,
      statusText: String(status),
      headers: new AxiosHeaders(),
      config,
    }
    if (status >= 400) {
      throw new AxiosError(
        'Request failed with status code ' + status,
        'ERR_BAD_REQUEST',
        config,
        null,
        response
      )
    }
    return response
  }
}

test('option writes surface the backend message of HTTP 400 responses', async () => {
  api.defaults.adapter = adapter(400, {
    success: false,
    message: '批量接口不允许更新该设置',
  })
  await assert.rejects(
    updateSystemOptionsBulk({ options: [{ key: 'Nope', value: '1' }] }),
    (error: Error) => error.message === '批量接口不允许更新该设置'
  )
})

test('committed but unpublished option writes are recognizable', async () => {
  api.defaults.adapter = adapter(200, {
    success: false,
    message: '设置已保存，但当前进程未能立即生效',
    data: { saved: true },
  })
  await assert.rejects(
    updateSystemOption({ key: 'SystemName', value: 'x' }),
    OptionSavedNotPublishedError
  )
})

test('plain business failures stay ordinary errors', async () => {
  api.defaults.adapter = adapter(200, { success: false, message: 'invalid' })
  await assert.rejects(
    updateSystemOption({ key: 'SystemName', value: 'x' }),
    (error: Error) =>
      !(error instanceof OptionSavedNotPublishedError) &&
      error.message === 'invalid'
  )
})
