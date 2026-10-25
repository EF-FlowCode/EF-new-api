import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  getRequestErrorMessage,
} from '@/lib/api'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'

type Report = {
  source_hash: string
  blocked: boolean
  already_applied: boolean
  entries:
    | {
        model: string
        plugin?: string
        channel_id?: number
        status: string
        reason?: string
      }[]
    | null
}
const reasons: Record<string, string> = {
  declared_task_without_fixed_binding:
    'Declared task model requires a fixed plugin binding',
  orphan_pricing_requires_disposition:
    'Orphan price needs an explicit decision',
  task_capable_channel_without_fixed_binding:
    'Channel needs a fixed task binding or an explicit non-task decision',
  fixed_source_missing_or_changed: 'Fixed plugin source is missing or changed',
  fixed_source_invalid: 'Fixed plugin source is invalid',
  binding_channel_missing: 'Bound channel is missing',
  binding_channel_type_mismatch: 'Binding does not match the channel type',
  empty_bound_model_list: 'Bound channel has no declared models',
  invalid_model_mapping: 'Model mapping is invalid',
  conflicting_fixed_model_contracts:
    'Bound models require conflicting pricing rules',
  missing_price_requires_user_or_site_policy:
    'Missing price depends on user or site policy',
  flat_token_rule_cannot_price_task:
    'A flat token expression cannot price this task',
}

type Entry = NonNullable<Report['entries']>[number]
// Neutral copy for the non-problem statuses; blocked entries use `reasons`.
const statuses: Record<string, string> = {
  ready: 'Original price and settlement preserved',
  follows_model_price: 'Follows the model price; no price is copied',
  converted_to_follow:
    'Identical plugin price removed; follows the model price again',
  override_differs_from_model:
    'Plugin price kept because it differs from the model price; review it in the model price dialog',
  preserved_unbound:
    'Price preserved; task requests remain unavailable without a fixed binding',
  preserved_unreferenced:
    'Unused price preserved; plugin tasks will follow it once a channel uses the model',
}
function entryMessage(entry: Entry) {
  if (entry.status === 'blocked')
    return (
      reasons[entry.reason ?? ''] ??
      entry.reason ??
      'Task price migration needs review'
    )
  return statuses[entry.status] ?? 'Outside the fixed task plugin'
}
const entryTone: Record<string, string> = {
  blocked: 'text-destructive',
  override_differs_from_model: 'text-amber-700 dark:text-amber-400',
}

export function TaskPricingMigrationPanel() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [report, setReport] = useState<Report | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const run = async (apply: boolean) => {
    setBusy(true)
    setError('')
    const source = report?.source_hash
    setReport(null)
    try {
      const response = apply
        ? await api.post<{ success: boolean; message?: string; data: Report }>(
            '/api/option/task_pricing_migration',
            { source_hash: source },
            CALLER_HANDLED_REQUEST_CONFIG
          )
        : await api.get<{ success: boolean; message?: string; data: Report }>(
            '/api/option/task_pricing_migration',
            CALLER_HANDLED_REQUEST_CONFIG
          )
      if (!response.data.success) throw new Error(response.data.message)
      setReport({
        ...response.data.data,
        already_applied: apply || response.data.data.already_applied,
      })
    } catch (cause) {
      setError(getRequestErrorMessage(cause, t('Failed to update setting')))
    } finally {
      if (apply)
        await Promise.all([
          queryClient.invalidateQueries({ queryKey: ['system-options'] }),
          queryClient.invalidateQueries({ queryKey: ['pricing'] }),
        ])
      setBusy(false)
    }
  }
  return (
    <div className='flex flex-col gap-3'>
      <Alert>
        <AlertDescription>
          {t(
            'Preview all task prices, including disabled bindings. Migration copies no prices: tasks keep following the model price unless a plugin price is set separately. It only stops tasks from silently using a mapped model price, and is blocked until every uncertain entry is resolved.'
          )}
        </AlertDescription>
      </Alert>
      <div className='flex flex-wrap gap-2'>
        <Button
          variant='outline'
          disabled={busy}
          onClick={() => {
            void run(false)
          }}
        >
          {t('Preview task price migration')}
        </Button>
        {report && !report.blocked && !report.already_applied && (
          <Button
            disabled={busy}
            onClick={() => {
              void run(true)
            }}
          >
            {t('Confirm task price migration')}
          </Button>
        )}
      </div>
      {error && (
        <Alert variant='destructive'>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      )}
      {report && (
        <Alert>
          <AlertDescription>
            <p>
              {report.already_applied
                ? t('Task price migration is complete')
                : report.blocked
                  ? t('Task price migration needs review')
                  : t('All task prices have a migration candidate')}
            </p>
            <ul className='list-disc pl-5'>
              {report.entries?.map((entry, index) => (
                <li
                  key={index}
                  data-status={entry.status}
                  className={cn(entryTone[entry.status])}
                >
                  {entry.model} {entry.plugin && `(${entry.plugin})`}{' '}
                  {entry.channel_id && `#${entry.channel_id}`}:{' '}
                  {t(entryMessage(entry))}
                </li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}
    </div>
  )
}
