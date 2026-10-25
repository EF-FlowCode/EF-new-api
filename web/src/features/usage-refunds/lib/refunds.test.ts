import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  BILLING_ANOMALY,
  isRefundableAnomaly,
  manualRefundSelection,
  parseRequestIDs,
  refundQuotaFromInput,
  refundFilter,
  type BillingAnomalyItem,
  type RefundCandidate,
} from './refunds'

describe('root balance return selection', () => {
  test('pasted request IDs accept a spreadsheet column, CSV or JSON array', () => {
    assert.deepEqual(
      parseRequestIDs('RequestID\r\nreq-a\r\n\r\nreq-b\r\nreq-a\r\n'),
      ['req-a', 'req-b']
    )
    assert.deepEqual(parseRequestIDs('["req-a", "req-b"]'), ['req-a', 'req-b'])
    assert.deepEqual(parseRequestIDs('req-a，req-b; req-c'), [
      'req-a',
      'req-b',
      'req-c',
    ])
    assert.deepEqual(parseRequestIDs('  '), [])
  })
  test('oversized request ID lists fail closed', () => {
    assert.throws(() => parseRequestIDs('x'.repeat(129)))
    assert.throws(() =>
      parseRequestIDs(
        Array.from({ length: 5001 }, (_, i) => `req-${i}`).join('\n')
      )
    )
  })
  test('filter includes every selection field and drops pagination and sorting', () => {
    const filter = refundFilter(
      {
        start_timestamp: 100,
        end_timestamp: 200,
        group: 'vip',
        username: 'alice',
        user_id: 1,
        channel: 4,
        token_name: 'test',
        model_name: 'model',
        request_id: 'r',
        type: 2,
        p: 9,
        page_size: 10,
        sort_by: 'quota',
      },
      1
    )
    assert.deepEqual(filter, {
      start_timestamp: 100,
      end_timestamp: 200,
      group: 'vip',
      username: 'alice',
      user_id: 1,
      channel: 4,
      token_name: 'test',
      model_name: 'model',
      request_id: 'r',
      type: 2,
      max_completion_tokens: 1,
    })
  })
  // 500000 quota per display unit, shown with 4 decimals.
  const convert = {
    toQuota: (amount: number) => Math.round(amount * 500000),
    fromQuota: (quota: number) => Number((quota / 500000).toFixed(4)),
  }
  const candidate = (log_id: number, quota: number, status = 'eligible') =>
    ({ log_id, quota, status }) as RefundCandidate
  test('manual return converts display amounts and keeps only partial ones', () => {
    const items = [
      candidate(3, 300000),
      candidate(1, 200000),
      candidate(2, 9, 'already_refunded'),
    ]
    assert.deepEqual(
      manualRefundSelection(items, { 3: '0.3', 1: '' }, convert),
      { log_ids: [1, 3], amounts: { '3': 150000 } }
    )
    assert.deepEqual(manualRefundSelection(items, { 3: '0.6' }, convert), {
      log_ids: [3],
    })
    const invalid: Record<number, string>[] = [
      {},
      { 3: '0' },
      { 3: '0.61' },
      { 3: '-0.1' },
      { 3: '1e-3' },
      { 3: 'abc' },
      { 3: '0.0000001' },
      { 2: '' },
      { 9: '' },
    ]
    for (const picks of invalid) {
      assert.throws(() => manualRefundSelection(items, picks, convert))
    }
  })
  test('typing the displayed charge returns the full charge despite rounding', () => {
    // 300001 quota displays as 0.6; typing 0.6 must not return 300000.
    const item = candidate(7, 300001)
    assert.equal(refundQuotaFromInput(item, '0.6', convert), 300001)
    assert.equal(refundQuotaFromInput(item, '', convert), 300001)
    assert.equal(refundQuotaFromInput(item, '0.3', convert), 150000)
  })
})

describe('billing anomaly refund eligibility', () => {
  const anomaly = (patch: Partial<BillingAnomalyItem>): BillingAnomalyItem => ({
    log_id: 1,
    request_id: '',
    created_at: 0,
    user_id: 1,
    username: 'u',
    model_name: 'claude',
    group: '',
    channel: 1,
    prompt_tokens: 0,
    completion_tokens: 0,
    use_time: 0,
    quota: 100,
    anomaly: BILLING_ANOMALY.inputEstimated,
    status: 'eligible',
    ...patch,
  })
  test('estimated charges can move into a refund batch', () => {
    assert.equal(isRefundableAnomaly(anomaly({})), true)
    assert.equal(
      isRefundableAnomaly(
        anomaly({ anomaly: BILLING_ANOMALY.outputEstimated })
      ),
      true
    )
  })
  test('free, uncharged or already returned records are audit-only', () => {
    assert.equal(
      isRefundableAnomaly(
        anomaly({ anomaly: BILLING_ANOMALY.noUpstreamUsage })
      ),
      false
    )
    assert.equal(isRefundableAnomaly(anomaly({ quota: 0 })), false)
    assert.equal(
      isRefundableAnomaly(anomaly({ status: 'already_refunded' })),
      false
    )
    assert.equal(
      isRefundableAnomaly(anomaly({ status: 'separate_billing' })),
      false
    )
  })
})
