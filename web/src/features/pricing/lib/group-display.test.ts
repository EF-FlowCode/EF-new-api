import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import {
  formatGroupLabel,
  formatGroupRatio,
  getGroupLayoutRow,
} from './group-display'

describe('pricing group display', () => {
  test('formats known tier prefixes without changing the stored group key', () => {
    assert.equal(formatGroupLabel('0_Tools_Nano_Banana'), 'Tools · Nano Banana')
    assert.equal(formatGroupLabel('1_vip_claude_Aws'), 'VIP · Claude AWS')
    assert.equal(formatGroupLabel('2_free_gemini'), 'Free · Gemini')
  })

  test('keeps unknown groups readable', () => {
    assert.equal(
      formatGroupLabel('custom_internal_group'),
      'Custom Internal Group'
    )
    assert.equal(formatGroupLabel('3_welfare_china'), 'Welfare China')
  })

  test('uses a numeric key prefix as the configured layout row', () => {
    assert.equal(getGroupLayoutRow('0_Tools_Nano_Banana'), 0)
    assert.equal(getGroupLayoutRow('1_vip_claude_Aws'), 1)
    assert.equal(getGroupLayoutRow('12_custom_group'), 12)
    assert.equal(getGroupLayoutRow('svip_TTS_test'), undefined)
  })

  test('renders ratios with the multiplier suffix', () => {
    assert.equal(formatGroupRatio(0.2), '0.2x')
    assert.equal(formatGroupRatio(0.9), '0.9x')
    assert.equal(formatGroupRatio(1), '1x')
    assert.equal(formatGroupRatio(undefined), undefined)
  })
})
