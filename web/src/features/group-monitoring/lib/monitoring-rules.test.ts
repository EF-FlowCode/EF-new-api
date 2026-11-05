import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  applyMonitoringToggles,
  resolveMonitoringGroups,
  unmatchedPatternRules,
} from './monitoring-rules.ts'

const known = [
  'auto',
  '0_Tools_Sound',
  '1_vip_codex',
  '1_vip_meta',
  '2_free_claude',
  'test_codex',
  'work_china',
]

test('resolve matches backend rule semantics', () => {
  assert.deepEqual(resolveMonitoringGroups(['0_*', '1_*', '2_*'], known), [
    '0_Tools_Sound',
    '1_vip_codex',
    '1_vip_meta',
    '2_free_claude',
  ])
  assert.deepEqual(resolveMonitoringGroups(['1_*', '!1_vip_meta'], known), [
    '1_vip_codex',
  ])
  assert.deepEqual(
    resolveMonitoringGroups(['*', '!test_*', '!work_*', 'test_codex'], known),
    ['0_Tools_Sound', '1_vip_codex', '1_vip_meta', '2_free_claude', 'test_codex']
  )
  assert.deepEqual(
    resolveMonitoringGroups(['1_vip_meta', '!1_vip_meta'], known),
    []
  )
  assert.deepEqual(resolveMonitoringGroups(['gone_group'], known), [
    'gone_group',
  ])
})

test('unmatched pattern rules are reported', () => {
  assert.deepEqual(unmatchedPatternRules(['1_*', '9_*', '!x_*', 'a'], known), [
    '9_*',
    '!x_*',
  ])
})

test('toggles keep prefix rules and add minimal overrides', () => {
  const rules = ['1_*']
  const on = new Set(['1_vip_codex', 'test_codex'])
  const next = applyMonitoringToggles(rules, known, on)
  assert.deepEqual(next, ['1_*', '!1_vip_meta', 'test_codex'])
  assert.deepEqual(resolveMonitoringGroups(next, known), [
    '1_vip_codex',
    'test_codex',
  ])
})

test('toggling back removes the override instead of stacking rules', () => {
  const rules = ['1_*', '!1_vip_meta', 'test_codex']
  const on = new Set(['1_vip_codex', '1_vip_meta'])
  assert.deepEqual(applyMonitoringToggles(rules, known, on), ['1_*'])
})

test('legacy exact list behaves like plain checkboxes', () => {
  const next = applyMonitoringToggles(
    ['1_vip_codex'],
    known,
    new Set(['1_vip_meta'])
  )
  assert.deepEqual(next, ['1_vip_meta'])
})
