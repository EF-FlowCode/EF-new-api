import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { ENDPOINT_TYPES } from '../constants'
import type { PricingModel } from '../types'
import { countModelsByEndpointType, filterByEndpointType } from './filters'

const models: PricingModel[] = [
  {
    id: 1,
    model_name: 'seed-a',
    model_ratio: 1,
    completion_ratio: 1,
    quota_type: 0,
    enable_groups: ['1_vip_china'],
    supported_endpoint_types: ['openai'],
  },
  {
    id: 2,
    model_name: 'chat-a',
    model_ratio: 1,
    completion_ratio: 1,
    quota_type: 0,
    enable_groups: ['1_vip_china'],
    supported_endpoint_types: ['openai'],
  },
]

describe('group endpoint overrides', () => {
  const overrides = {
    '1_vip_china': {
      type: 'openai',
      path: '/v1/chat/completions',
      method: 'POST',
    },
  }

  test('filters the whole selected group by its effective endpoint type', () => {
    assert.deepEqual(
      filterByEndpointType(models, 'openai', '1_vip_china', overrides),
      models
    )
    assert.deepEqual(
      filterByEndpointType(models, 'anthropic', '1_vip_china', overrides),
      []
    )
  })

  test('counts effective endpoint options consistently', () => {
    assert.equal(
      countModelsByEndpointType(models, 'openai', '1_vip_china', overrides),
      models.length
    )
    assert.equal(
      countModelsByEndpointType(
        models,
        ENDPOINT_TYPES.ALL,
        '1_vip_china',
        overrides
      ),
      models.length
    )
  })

  test('uses the OpenAI fallback when the selected group has no override', () => {
    assert.deepEqual(
      filterByEndpointType(models, 'openai', '0_Tools_WOS', overrides),
      models
    )
    assert.deepEqual(
      filterByEndpointType(models, 'anthropic', '0_Tools_WOS', overrides),
      []
    )
  })

  test('does not collapse a Claude model to the OpenAI fallback', () => {
    const claudeModel: PricingModel = {
      id: 3,
      model_name: 'claude-fable-5',
      model_ratio: 1,
      completion_ratio: 1,
      quota_type: 0,
      enable_groups: ['1_vip_claude_API2'],
      supported_endpoint_types: ['anthropic', 'openai'],
    }

    assert.deepEqual(
      filterByEndpointType([claudeModel], 'anthropic', '1_vip_claude_API2', {}),
      [claudeModel]
    )
    assert.equal(
      countModelsByEndpointType(
        [claudeModel],
        'openai',
        '1_vip_claude_API2',
        {}
      ),
      1
    )
  })
})
