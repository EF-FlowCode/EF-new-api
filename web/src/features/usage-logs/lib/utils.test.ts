import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { buildApiParams, getStatsSearchParams } from './utils'

describe('usage log statistics parameters', () => {
  test('removes pagination while preserving active filters', () => {
    const filters = getStatsSearchParams({
      page: 7,
      pageSize: 100,
      model: 'gpt-4.1',
      group: 'premium',
      startTime: 1_700_000_000_000,
      endTime: 1_700_000_060_000,
    })

    assert.deepEqual(filters, {
      model: 'gpt-4.1',
      group: 'premium',
      startTime: 1_700_000_000_000,
      endTime: 1_700_000_060_000,
    })
    assert.deepEqual(
      buildApiParams({
        page: 1,
        pageSize: 1,
        searchParams: filters,
        isAdmin: false,
      }),
      {
        p: 1,
        page_size: 1,
        model_name: 'gpt-4.1',
        group: 'premium',
        start_timestamp: 1_700_000_000,
        end_timestamp: 1_700_000_060,
      }
    )
  })
})
