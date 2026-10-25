import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import type { PricingModel } from '../types'
import {
  formatContextWindow,
  getEffectiveEndpointTypes,
  resolveEndpointOverride,
  resolveModelIcon,
  resolveModelEndpointOverride,
} from './model-helpers'

describe('formatContextWindow', () => {
  test('formats token windows using readable K/M units', () => {
    assert.equal(formatContextWindow(128000), '128K')
    assert.equal(formatContextWindow(262144), '262.1K')
    assert.equal(formatContextWindow(1000000), '1M')
  })

  test('hides missing or invalid windows', () => {
    assert.equal(formatContextWindow(undefined), null)
    assert.equal(formatContextWindow(0), null)
    assert.equal(formatContextWindow(-1), null)
  })
})

describe('resolveModelIcon', () => {
  test('prefers model metadata icon over vendor icon', () => {
    assert.equal(
      resolveModelIcon({
        model_name: 'muse-spark-1.2-contributor',
        icon: 'Meta.Color',
        vendor_icon: 'OpenAI.Color',
      } as PricingModel),
      'Meta.Color'
    )
  })

  test('falls back to vendor icon when model icon is absent', () => {
    assert.equal(
      resolveModelIcon({
        model_name: 'gpt-5',
        vendor_icon: 'OpenAI.Color',
      } as PricingModel),
      'OpenAI.Color'
    )
  })

  test('returns undefined when neither icon is configured', () => {
    assert.equal(
      resolveModelIcon({ model_name: 'custom-model' } as PricingModel),
      undefined
    )
  })
})

describe('pricing endpoint resolution', () => {
  test('keeps known model endpoints when no group override is configured', () => {
    assert.deepEqual(
      getEffectiveEndpointTypes(
        {
          model_name: 'claude-fable-5',
          supported_endpoint_types: ['anthropic', 'openai'],
        } as PricingModel,
        {},
        '1_vip_claude_API2'
      ),
      ['anthropic', 'openai']
    )
    assert.equal(resolveEndpointOverride({}, '1_vip_claude_API2'), undefined)
    assert.equal(
      resolveModelEndpointOverride(
        {
          model_name: 'claude-fable-5',
          supported_endpoint_types: ['anthropic', 'openai'],
        } as PricingModel,
        { default: { type: 'openai', path: '/v1/chat/completions' } },
        '1_vip_claude_API2'
      ),
      undefined
    )
  })

  test('uses OpenAI fallback only when model endpoints are unknown', () => {
    assert.deepEqual(
      getEffectiveEndpointTypes(
        { model_name: 'legacy-model' } as PricingModel,
        {},
        ''
      ),
      ['openai']
    )
  })
})
