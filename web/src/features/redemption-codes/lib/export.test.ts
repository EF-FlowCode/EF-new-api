import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  buildRedemptionExportContent,
  redemptionExportMimeType,
  type RedemptionExportOptions,
} from './export'

const data = {
  keys: ['KEY-1', 'KEY-2'],
  name: 'promo',
  quota: '$10.00',
}

const headers = { name: 'Name', code: 'Code', quota: 'Quota' }

const options = (
  overrides: Partial<RedemptionExportOptions> = {}
): RedemptionExportOptions => ({
  format: 'txt',
  includeName: true,
  includeQuota: true,
  headers,
  ...overrides,
})

describe('redemption code file export', () => {
  test('TXT writes one tab-separated line per code without a header', () => {
    assert.equal(
      buildRedemptionExportContent(data, options()),
      'promo\tKEY-1\t$10.00\npromo\tKEY-2\t$10.00\n'
    )
  })

  test('optional columns can be left out; the code is always kept', () => {
    assert.equal(
      buildRedemptionExportContent(
        data,
        options({ includeName: false, includeQuota: false })
      ),
      'KEY-1\nKEY-2\n'
    )
    assert.equal(
      buildRedemptionExportContent(data, options({ includeQuota: false })),
      'promo\tKEY-1\npromo\tKEY-2\n'
    )
  })

  test('Markdown writes a table with translated headers', () => {
    assert.equal(
      buildRedemptionExportContent(
        data,
        options({
          format: 'md',
          includeQuota: false,
          headers: { name: '名称', code: '兑换码', quota: '额度' },
        })
      ),
      '| 名称 | 兑换码 |\n| --- | --- |\n| promo | KEY-1 |\n| promo | KEY-2 |\n'
    )
  })

  test('cells cannot break TXT columns or Markdown table syntax', () => {
    const tricky = { keys: ['A|B'], name: 'a\tb\nc *x*', quota: '<1>&' }
    assert.equal(
      buildRedemptionExportContent(tricky, options()),
      'a b c *x*\tA|B\t<1>&\n'
    )
    assert.equal(
      buildRedemptionExportContent(tricky, options({ format: 'md' })),
      '| Name | Code | Quota |\n| --- | --- | --- |\n| a b c \\*x\\* | A\\|B | \\<1\\>&amp; |\n'
    )
  })

  test('file type follows the chosen format', () => {
    assert.equal(redemptionExportMimeType('txt'), 'text/plain;charset=utf-8')
    assert.equal(redemptionExportMimeType('md'), 'text/markdown;charset=utf-8')
  })
})
