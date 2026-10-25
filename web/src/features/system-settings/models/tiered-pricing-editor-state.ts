export type EditorPropSnapshot = {
  modelName?: string
  billingExpr: string
  requestRuleExpr: string
}

export type LastEmittedEditorValues = {
  billingExpr?: string
  requestRuleExpr?: string
}

export type EditorPropUpdate = {
  changed: boolean
  billingEcho: boolean
  requestRuleEcho: boolean
  shouldReset: boolean
}

export function classifyEditorPropUpdate(
  previous: EditorPropSnapshot,
  current: EditorPropSnapshot,
  lastEmitted: LastEmittedEditorValues
): EditorPropUpdate {
  const modelChanged = previous.modelName !== current.modelName
  const billingChanged = previous.billingExpr !== current.billingExpr
  const requestRuleChanged =
    previous.requestRuleExpr !== current.requestRuleExpr
  const billingEcho =
    !modelChanged &&
    billingChanged &&
    lastEmitted.billingExpr === current.billingExpr
  const requestRuleEcho =
    !modelChanged &&
    requestRuleChanged &&
    lastEmitted.requestRuleExpr === current.requestRuleExpr

  return {
    changed: modelChanged || billingChanged || requestRuleChanged,
    billingEcho,
    requestRuleEcho,
    shouldReset:
      modelChanged ||
      (billingChanged && !billingEcho) ||
      (requestRuleChanged && !requestRuleEcho),
  }
}
