import { AxiosHeaders, type AxiosAdapter } from 'axios'
import assert from 'node:assert/strict'
import { afterEach, test } from 'node:test'
import { api, type ApiRequestConfig } from '@/lib/api'
import {
  calculateAmount,
  calculateStripeAmount,
  calculateWaffoPancakeAmount,
  requestCreemPayment,
  requestPayment,
  requestStripePayment,
  requestWaffoPancakePayment,
  requestWaffoPayment,
} from './api'

const originalAdapter = api.defaults.adapter

afterEach(() => {
  api.defaults.adapter = originalAdapter
})

test('wallet payment requests leave business and HTTP error notification to their hooks', async () => {
  const configs: ApiRequestConfig[] = []
  const adapter: AxiosAdapter = async (config) => {
    configs.push(config as ApiRequestConfig)
    return {
      data: { success: false, message: 'Payment rejected' },
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      config,
    }
  }
  api.defaults.adapter = adapter

  await calculateAmount({ amount: 1 })
  await calculateStripeAmount({ amount: 1 })
  await requestPayment({ amount: 1, payment_method: 'epay' })
  await requestStripePayment({ amount: 1, payment_method: 'stripe' })
  await requestCreemPayment({ product_id: 'product', payment_method: 'creem' })
  await requestWaffoPayment({ amount: 1 })
  await calculateWaffoPancakeAmount({ amount: 1 })
  await requestWaffoPancakePayment({ amount: 1 })

  assert.equal(configs.length, 8)
  for (const config of configs) {
    assert.equal(config.skipBusinessError, true)
    assert.equal(config.skipErrorNotification, true)
  }
})
