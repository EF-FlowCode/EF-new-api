// Browser-only fixture: renders production components, without an API or database.
/* eslint-disable react-refresh/only-export-components -- Standalone test entry point. */
import { StrictMode, useState } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import { toAmountExpression } from '../src/features/pricing/lib/expression-units'
import type {
  BillingUsageInfo,
  TaskBillingRule,
} from '../src/features/pricing/types'
import {
  ModelRatioDialog,
  type ModelRatioData,
} from '../src/features/system-settings/models/model-ratio-dialog'
import { ModelTaskPluginPricing } from '../src/features/system-settings/models/task-pricing-editor'
import { TaskPricingMigrationPanel } from '../src/features/system-settings/models/task-pricing-migration-panel'
import { TaskPricingOverview } from '../src/features/system-settings/models/task-pricing-overview'
import {
  pluginTaskPricing,
  taskPricingOverview,
  type TaskExpressionMap,
  type TaskPricingModel,
} from '../src/features/system-settings/models/task-pricing-sources'
import { TieredPricingEditor } from '../src/features/system-settings/models/tiered-pricing-editor'
import { api } from '../src/lib/api'

await i18n.use(initReactI18next).init({
  lng: 'en',
  fallbackLng: 'en',
  resources: {},
  interpolation: { escapeValue: false },
})

const params = new URLSearchParams(location.search)
const fixtureQueryClient = new QueryClient({
  defaultOptions: { queries: { retry: false } },
})
if (params.has('migration')) {
  api.defaults.adapter = async (config) => {
    const applying = config.method === 'post'
    if (applying)
      document.documentElement.dataset.migrationRequest = String(config.data)
    const stale = applying && params.get('migration') === 'stale'
    const blocked = params.get('migration') === 'blocked'
    return {
      status: 200,
      statusText: 'OK',
      headers: {},
      config,
      data: stale
        ? { success: false, message: 'fixture preview stale' }
        : {
            success: true,
            data: {
              source_hash: 'a'.repeat(64),
              blocked,
              already_applied: applying,
              entries: [
                {
                  model: 'alias',
                  plugin: 'happyhorse',
                  channel_id: 1,
                  status: blocked ? 'blocked' : 'ready',
                  reason: blocked
                    ? 'orphan_pricing_requires_disposition'
                    : 'original_unit_price',
                },
                ...(blocked
                  ? []
                  : [
                      {
                        model: 'same',
                        plugin: 'happyhorse',
                        channel_id: 1,
                        status: 'converted_to_follow',
                      },
                      {
                        model: 'differs',
                        plugin: 'happyhorse',
                        channel_id: 1,
                        status: 'override_differs_from_model',
                      },
                    ]),
              ],
            },
          },
    }
  }
}
const initialRule = params.get('rule') || ''
const expression = (body: string) =>
  params.has('amount') ? toAmountExpression(body) : body
const usageExpression = 'v2:u("seconds") * 0.01'
const base = params.has('usage')
  ? usageExpression
  : expression('tier("base", p * 2 + c * 3)')
const usage: BillingUsageInfo | undefined =
  params.get('usage') === 'ready'
    ? {
        status: 'ready',
        schema: {
          seconds: { type: 'number', unit: 'second' },
          resolution: { enum: ['720p', '1080p'] },
        },
        sources: [
          {
            plugin: 'happyhorse',
            version: '1.0.0',
            source_hash: 'fixture',
            model: 'happyhorse-1.0-t2v',
          },
        ],
      }
    : params.get('usage') === 'conflict'
      ? { status: 'conflict' }
      : undefined

const taskRule = (
  model: string,
  source: TaskBillingRule['source'],
  expression?: string
): TaskBillingRule => ({
  source,
  plugin: 'happyhorse',
  model,
  mapped_model: 'happyhorse-1.0-t2v',
  expression,
})
const taskModels: TaskPricingModel[] = (
  [
    ['alias', taskRule('alias', 'plugin_original', 'v2:0.1')],
    ['follower', taskRule('follower', 'model_original', 'v2:0.3')],
    ['blocked', taskRule('blocked', 'explicit_task_rule_required')],
  ] as [string, TaskBillingRule][]
).map(([model, rule]) => ({
  model_name: model,
  billing_usage: { ...usage!, plugins: { happyhorse: usage! } },
  task_pricing: { happyhorse: [rule] },
}))

function TaskFixture() {
  const [rules, setRules] = useState<TaskExpressionMap>({
    alias: {
      happyhorse: params.has('unit')
        ? {
            version: 1,
            kind: 'unit_price',
            unit_price: 0.000003,
            upstream_model: 'happyhorse-1.0-t2v',
            dimensions: 'provider_v1',
            settlement: 'submit',
          }
        : params.has('token')
          ? {
              version: 1,
              kind: 'token_completion',
              model_ratio: 0.1234567890123456,
              ratio_configured: true,
              upstream_model: 'happyhorse-1.0-t2v',
              dimensions: 'provider_v1',
              settlement: 'tokens',
            }
          : 'v2:0.1',
    },
    orphan: { happyhorse: 'v2:0' },
  })
  const [saved, setSaved] = useState('')
  const name = params.get('model') || 'alias'
  if (params.has('overview'))
    return (
      <>
        <TaskPricingOverview
          rows={taskPricingOverview(taskModels, rules)}
          onEditModel={setSaved}
        />
        <output data-testid='saved'>{saved}</output>
      </>
    )
  return (
    <>
      <ModelTaskPluginPricing
        modelName={name}
        plugins={pluginTaskPricing(
          taskModels.find((model) => model.model_name === name),
          name,
          rules
        )}
        lockedReason={params.has('locked') ? 'fixture locked' : undefined}
        onSave={async (plugin, expression) => {
          setSaved(JSON.stringify({ name, plugin, expression }))
          if (params.has('fail')) throw new Error('fixture save failure')
          setRules((old) => {
            const next = { ...old, [name]: { ...old[name] } }
            if (expression === null) delete next[name][plugin]
            else next[name][plugin] = expression
            return next
          })
        }}
      />
      <output data-testid='saved'>{saved}</output>
    </>
  )
}

function Fixture() {
  const [billing, setBilling] = useState(base)
  const [rule, setRule] = useState(initialRule)
  const [calls, setCalls] = useState<string[]>([])
  const [saved, setSaved] = useState('')
  const [taskSaved, setTaskSaved] = useState('')
  const [open, setOpen] = useState(true)
  const [editData] = useState<ModelRatioData>({
    name: 'pricing-rule-test',
    billingMode: 'tiered_expr',
    billingExpr: params.has('usage')
      ? usageExpression
      : expression(
          'len <= 100 ? tier("short", p * 2 + c * 3) : tier("long", p * 4 + c * 5)'
        ),
    requestRuleExpr: initialRule,
  })
  if (params.has('migration'))
    return (
      <QueryClientProvider client={fixtureQueryClient}>
        <TaskPricingMigrationPanel />
      </QueryClientProvider>
    )
  if (params.has('task')) return <TaskFixture />
  if (params.has('dialog')) {
    return (
      <>
        <output data-testid='saved'>{saved}</output>
        <output data-testid='closed'>{String(!open)}</output>
        <output data-testid='task-saved'>{taskSaved}</output>
        <ModelRatioDialog
          taskPricing={
            params.has('taskdialog')
              ? {
                  plugins: pluginTaskPricing(
                    {
                      ...taskModels[1],
                      model_name: 'pricing-rule-test',
                    },
                    'pricing-rule-test',
                    {}
                  ),
                  onSave: async (plugin, expression) =>
                    setTaskSaved(JSON.stringify({ plugin, expression })),
                }
              : undefined
          }
          open={open}
          onOpenChange={setOpen}
          editData={editData}
          billingUsageByModel={usage ? { 'pricing-rule-test': usage } : {}}
          onSave={(data) => setSaved(JSON.stringify(data))}
        />
      </>
    )
  }
  return (
    <>
      <TieredPricingEditor
        modelName='pricing-rule-test'
        billingUsage={usage}
        billingExpr={billing}
        requestRuleExpr={rule}
        onBillingExprChange={(next) => {
          setBilling(next)
          setCalls((old) => [...old, `billing:${next}`])
        }}
        onRequestRuleExprChange={(next) => {
          setRule(next)
          setCalls((old) => [...old, `rule:${next}`])
        }}
      />
      <button onClick={() => setSaved(JSON.stringify({ billing, rule }))}>
        Save fixture
      </button>
      <button
        onClick={() => {
          setBilling('tier("base", p * 7 + c * 8)')
          setRule('(hour("UTC") >= 22 && hour("UTC") < 5 ? 4 : 1)')
        }}
      >
        External refresh
      </button>
      <output data-testid='rule'>{rule}</output>
      <output data-testid='billing'>{billing}</output>
      <output data-testid='calls'>{JSON.stringify(calls)}</output>
      <output data-testid='saved'>{saved}</output>
    </>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Fixture />
  </StrictMode>
)
