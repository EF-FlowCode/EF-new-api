import { AxiosError, AxiosHeaders, type AxiosAdapter } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api } from '@/lib/api'
import { exportTicketInvoices, fetchTickets } from './api'

const originalAdapter = api.defaults.adapter
afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test('invoice title search uses server pagination, encoded input and owned cancellation', async () => {
  const controller = new AbortController()
  const seen: string[] = []
  const adapter: AxiosAdapter = async (config) => {
    seen.push(config.url || '')
    assert.equal(config.signal, controller.signal)
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      data: { success: true, data: { items: [], total: 0 } },
    }
  }
  api.defaults.adapter = adapter
  await fetchTickets(
    {
      page: 2,
      pageSize: 20,
      admin: true,
      type: 'invoice',
      companyName: ' 大学 & 医院 ',
    },
    controller.signal
  )
  const url = new URL(seen[0], 'http://test')
  assert.equal(url.pathname, '/api/ticket/admin')
  assert.equal(url.searchParams.get('company_name'), '大学 & 医院')
  assert.equal(url.searchParams.get('p'), '2')
  await fetchTickets(
    { page: 1, pageSize: 20, companyName: 'private' },
    controller.signal
  )
  assert.equal(
    new URL(seen[1], 'http://test').searchParams.has('company_name'),
    false
  )
})

test('export sends selected IDs once and accepts only a nonempty XLSX response', async () => {
  let calls = 0
  const blob = new Blob(['fixture'], {
    type: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
  })
  api.defaults.adapter = async (config) => {
    calls++
    assert.equal(config.method, 'post')
    assert.equal(config.url, '/api/ticket/admin/invoice-export')
    assert.deepEqual(JSON.parse(config.data), { ids: [2, 7] })
    return {
      config,
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders({
        'content-type': blob.type,
        'content-disposition':
          'attachment; filename="invoice-requests-20260928-120000.xlsx"',
      }),
      data: blob,
    }
  }
  const result = await exportTicketInvoices([2, 7])
  assert.equal(calls, 1)
  assert.equal(result.blob, blob)
  assert.equal(result.filename, 'invoice-requests-20260928-120000.xlsx')
})

test('JSON errors, HTTP failures and HTML fallbacks are never downloaded as invoices', async () => {
  for (const status of [200, 400]) {
    api.defaults.adapter = async (config) => {
      const response = {
        config,
        status,
        statusText: 'error',
        headers: new AxiosHeaders({ 'content-type': 'application/json' }),
        data: new Blob(
          [
            JSON.stringify({
              success: false,
              message:
                'Selected invoices are missing or incomplete; refresh the list and try again',
            }),
          ],
          { type: 'application/json' }
        ),
      }
      if (status >= 400)
        throw new AxiosError('failed', '', config, undefined, response)
      return response
    }
    await assert.rejects(
      exportTicketInvoices([1]),
      /Selected invoices are missing/
    )
  }
  api.defaults.adapter = async (config) => ({
    config,
    status: 200,
    statusText: 'OK',
    headers: new AxiosHeaders({ 'content-type': 'text/html' }),
    data: new Blob(['<html>fallback</html>'], { type: 'text/html' }),
  })
  await assert.rejects(exportTicketInvoices([1]), /Failed to export invoices/)
})
