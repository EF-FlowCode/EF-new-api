import type {
  BillingUsageInfo,
  PricingModel,
  TaskBillingRule,
} from '@/features/pricing/types'

export const TASK_EXPRESSIONS_OPTION_KEY = 'billing_setting.task_expressions'

export type TaskPriceRule = {
  version: 1
  kind: 'amount' | 'unit_price' | 'token_completion'
  expression?: string
  unit_price?: number
  model_ratio?: number
  ratio_configured?: boolean
  upstream_model?: string
  dimensions?: 'none' | 'provider_v1'
  settlement?: 'submit' | 'wan_duration' | 'tokens'
}
export type TaskExpressionMap = Record<
  string,
  Record<string, string | TaskPriceRule>
>
export type TaskPricingModel = Pick<
  PricingModel,
  'model_name' | 'billing_usage' | 'task_pricing'
>

/**
 * How a plugin task of one model is priced right now:
 * - follow: uses the model price (changes apply immediately)
 * - follow_mapped: uses the mapped model's price (pre-migration only)
 * - override: has its own plugin price (`task_expressions[model][plugin]`)
 * - mapped_override: uses the mapped model's plugin price
 * - needs_price: task requests are blocked until a price is configured
 * - unavailable: plugin usage is not ready, so no effective price is known
 */
export type TaskPricingStatus =
  | 'follow'
  | 'follow_mapped'
  | 'override'
  | 'mapped_override'
  | 'needs_price'
  | 'unavailable'

export function classifyTaskSource(source: string): TaskPricingStatus {
  switch (source) {
    case 'plugin_original':
      return 'override'
    case 'plugin_mapped':
      return 'mapped_override'
    case 'model_original':
    case 'legacy_original':
      return 'follow'
    case 'model_mapped':
      return 'follow_mapped'
    default:
      // explicit_task_rule_required, missing, the retired unreviewed_original
      // and anything unknown: never present an unpriced task as fine.
      return 'needs_price'
  }
}

// When channels map one model to several upstream models, surface the state
// that needs the most attention.
const STATUS_PRIORITY: TaskPricingStatus[] = [
  'needs_price',
  'mapped_override',
  'follow_mapped',
  'follow',
  'unavailable',
]

export type PluginTaskPricing = {
  plugin: string
  status: TaskPricingStatus
  override?: string | TaskPriceRule
  rules: TaskBillingRule[]
  usage?: BillingUsageInfo
}

export type TaskPricingOverviewRow = PluginTaskPricing & { model: string }

/** Returns null when the stored option is not a JSON object. */
export function parseTaskExpressions(
  raw: string | undefined
): TaskExpressionMap | null {
  try {
    const value: unknown = JSON.parse(raw?.trim() || '{}')
    return value && typeof value === 'object' && !Array.isArray(value)
      ? (value as TaskExpressionMap)
      : null
  } catch {
    return null
  }
}

export function pluginTaskPricing(
  model: TaskPricingModel | undefined,
  name: string,
  rules: TaskExpressionMap
): PluginTaskPricing[] {
  const overrides = rules[name] ?? {}
  const plugins = new Set([
    ...Object.keys(model?.billing_usage?.plugins ?? {}),
    ...Object.keys(model?.task_pricing ?? {}),
    ...Object.keys(overrides),
  ])
  return [...plugins].sort().map((plugin) => {
    const effective = model?.task_pricing?.[plugin] ?? []
    const override = Object.hasOwn(overrides, plugin)
      ? overrides[plugin]
      : undefined
    const statuses = effective.map((rule) => classifyTaskSource(rule.source))
    const status: TaskPricingStatus =
      override !== undefined
        ? 'override'
        : (STATUS_PRIORITY.find((item) => statuses.includes(item)) ??
          'unavailable')
    return {
      plugin,
      status,
      override,
      rules: effective,
      usage: model?.billing_usage?.plugins?.[plugin],
    }
  })
}

export function taskPricingOverview(
  models: TaskPricingModel[],
  rules: TaskExpressionMap
): TaskPricingOverviewRow[] {
  const byName = new Map(models.map((model) => [model.model_name, model]))
  const names = new Set([...byName.keys(), ...Object.keys(rules)])
  return [...names]
    .flatMap((name) =>
      pluginTaskPricing(byName.get(name), name, rules).map((entry) => ({
        ...entry,
        model: name,
      }))
    )
    .sort(
      (a, b) =>
        a.plugin.localeCompare(b.plugin) || a.model.localeCompare(b.model)
    )
}

export type ModelTaskPricingFlags = { override: boolean; needsPrice: boolean }

export function modelTaskPricingFlags(
  rows: TaskPricingOverviewRow[]
): Record<string, ModelTaskPricingFlags> {
  const flags: Record<string, ModelTaskPricingFlags> = {}
  for (const row of rows) {
    const flag = (flags[row.model] ??= { override: false, needsPrice: false })
    if (row.status === 'override') flag.override = true
    if (row.status === 'needs_price') flag.needsPrice = true
  }
  return flags
}

/** Upstream models the effective rules resolve through, excluding itself. */
export function mappedModels(
  entry: { rules: TaskBillingRule[] },
  name: string
) {
  return [
    ...new Set(
      entry.rules
        .map((rule) => rule.mapped_model)
        .filter((model) => model && model !== name)
    ),
  ]
}

/** The model whose price a mapped rule borrows (plugin_mapped/model_mapped). */
export function borrowedPriceModel(entry: PluginTaskPricing, name: string) {
  return entry.rules.find(
    (rule) =>
      classifyTaskSource(rule.source) === entry.status && rule.model !== name
  )?.model
}

export type TaskRuleSummary =
  | { kind: 'expression'; expression: string }
  | { kind: 'unit_price'; unitPrice: number }
  | { kind: 'token_completion'; modelRatio: number }
  | { kind: 'none' }

export function summarizeTaskRule(
  rule:
    | string
    | TaskPriceRule
    // The effective rule echoed by task_pricing_usage.
    | NonNullable<TaskBillingRule['rule']>
    | undefined
): TaskRuleSummary {
  if (typeof rule === 'string')
    return rule.trim()
      ? { kind: 'expression', expression: rule }
      : { kind: 'none' }
  if (!rule) return { kind: 'none' }
  if (rule.kind === 'unit_price' && rule.unit_price !== undefined)
    return { kind: 'unit_price', unitPrice: rule.unit_price }
  if (rule.kind === 'token_completion' && rule.model_ratio !== undefined)
    return { kind: 'token_completion', modelRatio: rule.model_ratio }
  return rule.expression?.trim()
    ? { kind: 'expression', expression: rule.expression }
    : { kind: 'none' }
}

/** Sets (or with null removes) one plugin rule without touching the others. */
export function applyTaskRuleChange(
  rules: TaskExpressionMap,
  model: string,
  plugin: string,
  value: string | TaskPriceRule | null
): TaskExpressionMap {
  const next = { ...rules, [model]: { ...rules[model] } }
  if (value === null) delete next[model][plugin]
  else next[model][plugin] = value
  if (Object.keys(next[model]).length === 0) delete next[model]
  return next
}
