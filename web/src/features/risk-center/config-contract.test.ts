import { AxiosHeaders } from 'axios'
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api } from '@/lib/api'
import {
  fetchModerationConfig,
  updateModerationConfig,
  fetchSubjects,
  fetchIncidents,
  fetchModerationIncidents,
  fetchEnforcementCounters,
  fetchEnforcementIncidents,
  detectIp,
  createRule,
  updateRule,
} from './api'
import {
  parseModerationConfig,
  parseModerationKeys,
} from './lib/config-contract'
import type { ModerationConfig, RiskRuleUpsertRequest } from './types'

const config: ModerationConfig = {
  enabled: true,
  mode: 'enforce',
  base_url: 'https://moderation.example',
  model: 'custom',
  api_keys: ['***abcd'],
  event_queue_size: 100,
  worker_count: 3,
  http_timeout_ms: 1500,
  max_retries: 1,
  redis_queue_enabled: true,
  sampling_rate_percent: 31,
  image_max_size_kb: 512,
  enabled_groups: ['paid'],
  group_modes: { paid: 'observe_only' },
  debug_result_retain_minutes: 4,
  flagged_retention_hours: 10,
  benign_retention_hours: 2,
  record_unmatched_inputs: true,
}

test('moderation config unwraps and preserves complete server fields on save', async () => {
  const previous = api.defaults.adapter
  let submitted: unknown
  api.defaults.adapter = async (request) => {
    if (request.method === 'put') submitted = JSON.parse(request.data)
    return {
      data: { success: true, data: { config, key_count: 1 } },
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      config: request,
    }
  }
  try {
    const loaded = await fetchModerationConfig()
    assert.deepEqual(loaded, config)
    await updateModerationConfig(loaded)
    assert.deepEqual(submitted, config)
  } finally {
    api.defaults.adapter = previous
  }
})

test('moderation invalid config and key drafts are rejected instead of clearing keys', () => {
  for (const invalid of [
    null,
    {},
    { config },
    { ...config, api_keys: '[]' },
    { ...config, http_timeout_ms: NaN },
  ]) {
    assert.throws(() => parseModerationConfig(invalid))
  }
  for (const invalid of ['', '{', 'null', '{}', '[1]'])
    assert.throws(() => parseModerationKeys(invalid))
  assert.deepEqual(parseModerationKeys('[]'), [])
  assert.deepEqual(parseModerationKeys('["***abcd","new-key"]'), [
    '***abcd',
    'new-key',
  ])
})

test('risk rule create and toggle submit arrays and normalize server creation time', async () => {
  const rule: RiskRuleUpsertRequest = {
    name: 'rule',
    description: '',
    enabled: true,
    scope: 'user',
    detector: 'distribution',
    match_mode: 'all',
    priority: 10,
    action: 'block',
    auto_block: true,
    auto_recover: true,
    recover_mode: 'ttl',
    recover_after_seconds: 60,
    response_status_code: 429,
    response_message: 'blocked',
    score_weight: 1,
    conditions: [{ metric: 'requests_1m', op: '>', value: 5 }],
    groups: ['paid'],
  }
  const previous = api.defaults.adapter
  const submitted: RiskRuleUpsertRequest[] = []
  api.defaults.adapter = async (request) => {
    submitted.push(JSON.parse(request.data))
    return {
      data: {
        success: true,
        data: {
          ...rule,
          id: 4,
          created_at: 123,
          conditions: JSON.stringify(rule.conditions),
          groups: JSON.stringify(rule.groups),
        },
      },
      status: 200,
      statusText: 'OK',
      headers: new AxiosHeaders(),
      config: request,
    }
  }
  try {
    const created = await createRule(rule)
    assert.equal(created?.created_time, 123)
    assert.deepEqual(created?.conditions, rule.conditions)
    assert.deepEqual(created?.groups, rule.groups)
    await updateRule(4, { ...rule, enabled: false })
    assert.deepEqual(submitted, [rule, { ...rule, enabled: false }])
  } finally {
    api.defaults.adapter = previous
  }
})

test('risk readers preserve real server fields including zero values and normalize null item lists', async () => {
  const previous = api.defaults.adapter
  let items: unknown = [
    {
      id: 11,
      subject_type: 'user',
      subject_id: 20,
      rule_hit_count_24h: 0,
      last_incident_at: 100,
      primary_rule: 'r',
      max_category: 'violence',
      max_score: 0,
      input_summary: 'text',
      created_at: 100,
      source: 'moderation',
      user_id: 20,
    },
  ]
  api.defaults.adapter = async (request) => ({
    data: {
      success: true,
      data: { items, page: 1, page_size: 10, total: items ? 1 : 0 },
    },
    status: 200,
    statusText: 'OK',
    headers: new AxiosHeaders(),
    config: request,
  })
  try {
    const query = { page: 1, pageSize: 10 }
    assert.equal((await fetchSubjects(query)).items[0].subject_id, 20)
    assert.equal((await fetchIncidents(query)).items[0].created_at, 100)
    assert.equal((await fetchModerationIncidents(query)).items[0].max_score, 0)
    assert.equal(
      (await fetchEnforcementIncidents(query)).items[0].source,
      'moderation'
    )
    items = null
    for (const fetch of [
      fetchSubjects,
      fetchIncidents,
      fetchModerationIncidents,
      fetchEnforcementCounters,
      fetchEnforcementIncidents,
    ])
      assert.deepEqual((await fetch(query)).items, [])
    api.defaults.adapter = async (request) => {
      assert.equal(request.method, 'get')
      assert.equal(request.data, undefined)
      return {
        data: { success: true, data: { ip: '127.0.0.1' } },
        status: 200,
        statusText: 'OK',
        headers: new AxiosHeaders(),
        config: request,
      }
    }
    assert.deepEqual(await detectIp(), { ip: '127.0.0.1' })
  } finally {
    api.defaults.adapter = previous
  }
})
