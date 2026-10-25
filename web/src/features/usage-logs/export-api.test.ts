import { AxiosError, AxiosHeaders, type AxiosAdapter } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api } from '@/lib/api'
import { exportUserLogs } from './api'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

function blobAdapter(
  status: number,
  body: Blob,
  contentType: string
): AxiosAdapter {
  return async (config) => {
    const response = {
      data: body,
      status,
      statusText: String(status),
      headers: new AxiosHeaders({ 'content-type': contentType }),
      config,
    }
    if (status >= 400) {
      throw new AxiosError(
        'Request failed',
        'ERR_BAD_RESPONSE',
        config,
        null,
        response
      )
    }
    return response
  }
}

const jsonBlob = (message: string) =>
  new Blob([JSON.stringify({ success: false, message })], {
    type: 'application/json',
  })

test('sync export surfaces backend JSON errors instead of downloading them', async () => {
  api.defaults.adapter = blobAdapter(
    200,
    jsonBlob('导出日志失败'),
    'application/json; charset=utf-8'
  )
  await assert.rejects(exportUserLogs(new URLSearchParams()), /导出日志失败/)
})

test('sync export reads the message from HTTP error blobs', async () => {
  api.defaults.adapter = blobAdapter(
    429,
    jsonBlob('请求过于频繁'),
    'application/json'
  )
  await assert.rejects(exportUserLogs(new URLSearchParams()), /请求过于频繁/)
})

test('sync export returns zip blobs', async () => {
  const zip = new Blob(['PK'], { type: 'application/zip' })
  api.defaults.adapter = blobAdapter(200, zip, 'application/zip')
  const { blob, filename } = await exportUserLogs(new URLSearchParams())
  assert.equal(blob, zip)
  assert.equal(filename, 'usage-logs.zip')
})
