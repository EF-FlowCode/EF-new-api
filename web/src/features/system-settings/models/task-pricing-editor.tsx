import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge } from '@/components/status-badge'
import type { BillingUsageInfo } from '@/features/pricing/types'
import {
  borrowedPriceModel,
  mappedModels,
  type PluginTaskPricing,
  type TaskPriceRule,
  type TaskPricingStatus,
} from './task-pricing-sources'
import { PluginUsageFields } from './tiered-pricing-editor'

const STATUS_BADGES: Record<
  TaskPricingStatus,
  { label: string; variant: 'success' | 'info' | 'neutral' | 'danger' }
> = {
  follow: { label: 'Follows model price', variant: 'success' },
  follow_mapped: { label: 'Follows mapped model price', variant: 'neutral' },
  override: { label: 'Set separately', variant: 'info' },
  mapped_override: {
    label: 'Uses mapped model plugin price',
    variant: 'neutral',
  },
  needs_price: { label: 'Price required', variant: 'danger' },
  unavailable: { label: 'Plugin usage unavailable', variant: 'neutral' },
}

export function TaskPricingStatusBadge({
  status,
}: {
  status: TaskPricingStatus
}) {
  const { t } = useTranslation()
  return (
    <StatusBadge
      label={t(STATUS_BADGES[status].label)}
      variant={STATUS_BADGES[status].variant}
      copyable={false}
    />
  )
}

/**
 * Plugin task prices of one model, shown inside the model price dialog. Each
 * plugin saves `billing_setting.task_expressions` on its own; this is separate
 * from the dialog's model price submit.
 */
export function ModelTaskPluginPricing({
  modelName,
  plugins,
  saving,
  lockedReason,
  onSave,
}: {
  modelName: string
  plugins: PluginTaskPricing[]
  saving?: boolean
  /** Set when saving now would discard other unsaved edits. */
  lockedReason?: string
  onSave: (
    plugin: string,
    expression: string | TaskPriceRule | null
  ) => Promise<void>
}) {
  const { t } = useTranslation()
  if (!plugins.length) return null
  return (
    <section className='space-y-3 border-t pt-4'>
      <div className='space-y-1'>
        <h3 className='text-sm font-medium'>{t('Task plugins')}</h3>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Plugin prices are saved immediately and separately from the model price.'
          )}
        </p>
      </div>
      {lockedReason && (
        <Alert>
          <AlertDescription>{lockedReason}</AlertDescription>
        </Alert>
      )}
      {plugins.map((entry) => (
        <PluginPricingItem
          key={entry.plugin}
          modelName={modelName}
          entry={entry}
          saving={saving}
          locked={!!lockedReason}
          onSave={(expression) => onSave(entry.plugin, expression)}
        />
      ))}
    </section>
  )
}

function PluginPricingItem({
  modelName,
  entry,
  saving,
  locked,
  onSave,
}: {
  modelName: string
  entry: PluginTaskPricing
  saving?: boolean
  locked?: boolean
  onSave: (expression: string | TaskPriceRule | null) => Promise<void>
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  const hasOverride = entry.override !== undefined
  const showForm = hasOverride || editing
  const borrowed = borrowedPriceModel(entry, modelName)
  const mapped = mappedModels(entry, modelName)
  const description = {
    follow: t(
      'Same as the model price above; price changes take effect immediately.'
    ),
    follow_mapped: t(
      'Follows the price of mapped model {{model}} until the task price migration is applied.',
      { model: borrowed }
    ),
    override: t(
      'Uses its own plugin price below; model price changes do not affect it.'
    ),
    mapped_override: t(
      'Uses the plugin-specific price of mapped model {{model}}.',
      { model: borrowed }
    ),
    needs_price: t(
      'Task requests are blocked until a price is set: set the model price above, or set a plugin price separately.'
    ),
    unavailable: t(
      'Plugin usage is unavailable, so the effective task price cannot be shown.'
    ),
  }[entry.status]
  return (
    <div
      className='space-y-3 rounded-md border p-3'
      data-testid={`task-plugin-${entry.plugin}`}
    >
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='font-medium'>{entry.plugin}</span>
          <TaskPricingStatusBadge status={entry.status} />
        </div>
        {!showForm && (
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={saving || locked}
            onClick={() => setEditing(true)}
          >
            {t('Set plugin price separately')}
          </Button>
        )}
        {editing && !hasOverride && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            disabled={saving}
            onClick={() => setEditing(false)}
          >
            {t('Cancel')}
          </Button>
        )}
      </div>
      <p className='text-muted-foreground text-sm'>{description}</p>
      {mapped.length > 0 && (
        <p className='text-muted-foreground text-xs'>
          {t('Mapped models')}: {mapped.join(', ')}
        </p>
      )}
      {showForm && (
        <TaskRuleForm
          key={JSON.stringify(entry.override ?? null)}
          expression={entry.override}
          profiles={entry.usage ? [entry.usage] : []}
          saving={saving || locked}
          onSave={async (expression) => {
            await onSave(expression)
            setEditing(false)
          }}
        />
      )}
    </div>
  )
}

function AmountRuleForm({
  expression,
  profiles,
  saving,
  onSave,
}: {
  expression?: string
  profiles: BillingUsageInfo[]
  saving?: boolean
  onSave: (expression: string | null) => Promise<void>
}) {
  const { t } = useTranslation()
  const id = useId()
  const [draft, setDraft] = useState(expression ?? '')
  // The shared mutation hook owns the error toast; retain the draft on failure.
  const save = (value: string | null) => {
    void onSave(value).catch(() => {})
  }
  return (
    <div className='flex flex-col gap-3'>
      <Label htmlFor={id}>{t('Plugin expression')}</Label>
      <Textarea
        id={id}
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        disabled={saving}
        placeholder='v2:u("seconds") * 0.01'
      />
      {profiles.length ? (
        profiles.map((profile, index) => (
          <PluginUsageFields key={index} usage={profile} required />
        ))
      ) : (
        <PluginUsageFields required />
      )}
      <div className='flex gap-2'>
        <Button
          disabled={saving || !draft.trim() || draft === expression}
          onClick={() => save(draft)}
        >
          {t('Save')}
        </Button>
        <RestoreModelPriceButton
          disabled={saving || expression === undefined}
          onConfirm={() => save(null)}
        />
      </div>
    </div>
  )
}

// Restoring deletes the plugin rule immediately; the task then follows the model
// price (or is blocked when the model has none), so ask first.
function RestoreModelPriceButton({
  disabled,
  onConfirm,
}: {
  disabled?: boolean
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button
        variant='outline'
        disabled={disabled}
        onClick={() => setOpen(true)}
      >
        {t('Restore following model price')}
      </Button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title={t('Restore following the model price?')}
        desc={t(
          'The plugin-specific price is deleted immediately and tasks follow the model price. If the model has no price, task requests are blocked until one is set.'
        )}
        confirmText={t('Restore')}
        destructive
        disabled={disabled}
        handleConfirm={() => {
          setOpen(false)
          onConfirm()
        }}
      />
    </>
  )
}

export function TaskRuleForm(props: {
  expression?: string | TaskPriceRule
  profiles: BillingUsageInfo[]
  saving?: boolean
  onSave: (expression: string | TaskPriceRule | null) => Promise<void>
}) {
  const { t } = useTranslation()
  const [convert, setConvert] = useState(false)
  const rule =
    typeof props.expression === 'object' ? props.expression : undefined
  if (rule && rule.kind !== 'amount' && !convert) {
    return (
      <LegacyTaskRuleForm
        rule={rule}
        saving={props.saving}
        onSave={props.onSave}
        convert={() => setConvert(true)}
      />
    )
  }
  return (
    <>
      {convert && (
        <Alert>
          <AlertDescription>
            {t(
              'Switching to a total amount changes rounding and completion billing. Enter the intended new price.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <AmountRuleForm
        profiles={props.profiles}
        saving={props.saving}
        onSave={props.onSave}
        expression={
          convert
            ? undefined
            : typeof props.expression === 'string'
              ? props.expression
              : rule?.expression
        }
      />
    </>
  )
}

function LegacyTaskRuleForm({
  rule,
  saving,
  onSave,
  convert,
}: {
  rule: TaskPriceRule
  saving?: boolean
  onSave: (rule: TaskPriceRule | null) => Promise<void>
  convert: () => void
}) {
  const { t } = useTranslation()
  const id = useId()
  const quotaPerUnit = useSystemConfigStore(
    (state) => state.config.currency.quotaPerUnit
  )
  const [unitBasis] = useState(quotaPerUnit)
  const initial =
    rule.kind === 'unit_price'
      ? String(rule.unit_price)
      : String((rule.model_ratio! * 1000000) / unitBasis)
  const [draft, setDraft] = useState(initial)
  const price = Number(draft)
  const save = () => {
    const next =
      rule.kind === 'unit_price'
        ? { ...rule, unit_price: price }
        : {
            ...rule,
            model_ratio: (price / 1000000) * unitBasis,
            settlement:
              rule.dimensions === 'none' ||
              !rule.ratio_configured ||
              price === 0
                ? ('submit' as const)
                : ('tokens' as const),
          }
    void onSave(next).catch(() => {})
  }
  return (
    <div className='flex flex-col gap-3'>
      <Alert>
        <AlertDescription>
          <p>{t('Original price and settlement preserved')}</p>
          <p>{rule.upstream_model}</p>
          <p>
            {rule.settlement === 'wan_duration'
              ? t('Settle using completed duration')
              : rule.settlement === 'tokens'
                ? t('Settle using completed tokens')
                : t('Keep the submitted amount')}
          </p>
          <p>
            {t(
              'Unit conversion is truncated before quantity multipliers. Saving without edits preserves the original calculation.'
            )}
          </p>
        </AlertDescription>
      </Alert>
      <Label htmlFor={id}>
        {rule.kind === 'unit_price'
          ? t('Unit price (USD)')
          : t('USD per million completion tokens')}
      </Label>
      <Input
        id={id}
        type='number'
        step='any'
        min='0'
        value={draft}
        disabled={saving}
        onChange={(event) => setDraft(event.target.value)}
      />
      <div className='flex flex-wrap gap-2'>
        <Button
          disabled={
            saving ||
            draft === initial ||
            !draft.trim() ||
            !Number.isFinite(price) ||
            price < 0
          }
          onClick={save}
        >
          {t('Save')}
        </Button>
        <Button variant='outline' disabled={saving} onClick={convert}>
          {t('Change to total amount')}
        </Button>
        <RestoreModelPriceButton
          disabled={saving}
          onConfirm={() => {
            void onSave(null).catch(() => {})
          }}
        />
      </div>
    </div>
  )
}
