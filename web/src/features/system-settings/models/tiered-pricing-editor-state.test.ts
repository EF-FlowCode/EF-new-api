import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { classifyEditorPropUpdate } from './tiered-pricing-editor-state'

const initial = {
  modelName: 'deepseek-chat',
  billingExpr: 'p * 1',
  requestRuleExpr:
    '(hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12 ? 2 : 1)',
}

describe('classifyEditorPropUpdate', () => {
  test('does not reset local state for an exact callback echo', () => {
    const current = {
      ...initial,
      requestRuleExpr:
        '(hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)',
    }

    assert.deepEqual(
      classifyEditorPropUpdate(initial, current, {
        requestRuleExpr: current.requestRuleExpr,
      }),
      {
        changed: true,
        billingEcho: false,
        requestRuleEcho: true,
        shouldReset: false,
      }
    )
  })

  test('resets for an external update to the same model', () => {
    const current = {
      ...initial,
      requestRuleExpr:
        '(hour("Asia/Shanghai") >= 8 && hour("Asia/Shanghai") < 10 ? 1.5 : 1)',
    }

    assert.equal(
      classifyEditorPropUpdate(initial, current, {}).shouldReset,
      true
    )
  })

  test('resets when one changed prop is external even if the other is an echo', () => {
    const current = {
      ...initial,
      billingExpr: 'p * 2',
      requestRuleExpr:
        '(hour("Asia/Shanghai") >= 21 || hour("Asia/Shanghai") < 6 ? 2 : 1)',
    }

    const update = classifyEditorPropUpdate(initial, current, {
      requestRuleExpr: current.requestRuleExpr,
    })
    assert.equal(update.requestRuleEcho, true)
    assert.equal(update.shouldReset, true)
  })

  test('always resets when the selected model changes', () => {
    assert.equal(
      classifyEditorPropUpdate(
        initial,
        { ...initial, modelName: 'deepseek-reasoner' },
        {}
      ).shouldReset,
      true
    )
  })
})
