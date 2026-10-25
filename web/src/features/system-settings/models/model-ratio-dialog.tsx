import { useEffect, useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { FlatTokenPriceFields } from '@/features/pricing/components/flat-token-price-fields'
import {
  buildFlatTokenPriceExpr,
  EMPTY_FLAT_TOKEN_PRICES,
  parseFlatTokenPriceExpr,
  ratioToFlatTokenPrices,
  type FlatTokenPrices,
} from '@/features/pricing/lib/flat-token-price'
import type { BillingUsageInfo } from '@/features/pricing/types'
import { ModelTaskPluginPricing } from './task-pricing-editor'
import type { PluginTaskPricing, TaskPriceRule } from './task-pricing-sources'
import { TieredPricingEditor } from './tiered-pricing-editor'

export type ModelRatioData = {
  preservePricing?: boolean
  name: string
  price?: string
  ratio?: string
  cacheRatio?: string
  createCacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
  knowledgeInput?: string
  knowledgeImage?: string
  knowledgeBatchInput?: string
  knowledge?: {
    kind: 'embedding' | 'rerank'
    multimodal: boolean
    batch_file: boolean
  }
  billingMode?:
    | 'per-token'
    | 'per-request'
    | 'flat_price'
    | 'tiered_expr'
    | 'audio_seconds'
  billingExpr?: string
  requestRuleExpr?: string
  /** Mode as stored, when billingMode shows a normalized display mode. */
  storedBillingMode?: string
}

type PricingMode = 'per-token' | 'per-request' | 'tiered_expr' | 'audio_seconds'
type Props = {
  billingUsageByModel?: Record<string, BillingUsageInfo | undefined>
  /** Plugin tasks priced from this model; saved separately from the model. */
  taskPricing?: {
    plugins: PluginTaskPricing[]
    saving?: boolean
    lockedReason?: string
    onSave: (
      plugin: string,
      expression: string | TaskPriceRule | null
    ) => Promise<void>
  }
  open: boolean
  onOpenChange: (open: boolean) => void
  onSave: (data: ModelRatioData) => void
  editData?: ModelRatioData | null
}

function initialState(data?: ModelRatioData | null) {
  const parsed =
    data &&
    (data.billingMode === 'flat_price' || data.billingMode === 'tiered_expr')
      ? parseFlatTokenPriceExpr(data.billingExpr || '')
      : null
  const mode: PricingMode =
    data?.billingMode === 'audio_seconds'
      ? 'audio_seconds'
      : data?.billingMode === 'tiered_expr' && !parsed
        ? 'tiered_expr'
        : !parsed && data?.price
          ? 'per-request'
          : 'per-token'
  const prices =
    parsed ||
    (data ? ratioToFlatTokenPrices(data) : { ...EMPTY_FLAT_TOKEN_PRICES })
  prices.batchInputPrice = data?.knowledgeBatchInput || ''
  return {
    mode,
    prices,
    name: data?.name || '',
    price: data?.price || '',
    expression: data?.billingExpr || '',
    request: data?.requestRuleExpr || '',
  }
}

export function ModelRatioDialog({
  open,
  onOpenChange,
  onSave,
  editData,
  billingUsageByModel,
  taskPricing,
}: Props) {
  const { t } = useTranslation()
  const formId = useId()
  const [state, setState] = useState(() => initialState(editData))
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setState(initialState(editData))
  }, [open, editData])
  const setPrices = (prices: FlatTokenPrices) =>
    setState((s) => ({ ...s, prices }))
  const validNumber = (value: string) =>
    value.trim() !== '' && Number.isFinite(Number(value)) && Number(value) >= 0
  const save = () => {
    if (!state.name.trim()) return
    // A no-op save preserves the migrated settlement rules and precision.
    if (
      editData &&
      JSON.stringify(state) === JSON.stringify(initialState(editData))
    ) {
      onOpenChange(false)
      return
    }
    const data: ModelRatioData = {
      name: state.name.trim(),
      billingMode: state.mode,
    }
    if (editData) {
      const original = initialState(editData)
      const withoutBatch = (value: typeof state) => ({
        ...value,
        prices: { ...value.prices, batchInputPrice: '' },
      })
      // A single-tier tiered_expr is shown as per-token; saving it unchanged
      // must still write flat_price so the stored mode matches the display.
      const normalizesStoredMode =
        state.mode === 'per-token' &&
        (editData.storedBillingMode ?? editData.billingMode) === 'tiered_expr'
      if (
        !normalizesStoredMode &&
        JSON.stringify(withoutBatch(state)) ===
          JSON.stringify(withoutBatch(original))
      ) {
        if (
          state.prices.batchInputPrice &&
          !validNumber(state.prices.batchInputPrice)
        )
          return
        onSave({
          ...editData,
          preservePricing: true,
          knowledgeBatchInput: state.prices.batchInputPrice || '',
        })
        onOpenChange(false)
        return
      }
    }
    if (state.mode === 'per-token') {
      const expression = buildFlatTokenPriceExpr(state.prices)
      if (
        !expression ||
        (state.prices.batchInputPrice &&
          !validNumber(state.prices.batchInputPrice))
      )
        return
      data.billingMode = 'flat_price'
      data.billingExpr = expression
      data.knowledgeBatchInput = state.prices.batchInputPrice || ''
    } else if (state.mode === 'audio_seconds') {
      if (!validNumber(state.prices.inputPrice)) return
      data.ratio = String(Number(state.prices.inputPrice) / 2)
    } else if (state.mode === 'per-request') {
      if (!validNumber(state.price)) return
      data.price = state.price
    } else {
      // An empty expression passes the backend but leaves the model unusable.
      if (!state.expression.trim()) {
        toast.error(t('Billing expression cannot be empty'))
        return
      }
      data.billingExpr = state.expression
      data.requestRuleExpr = state.request
      data.knowledgeBatchInput = state.prices.batchInputPrice || ''
    }
    onSave(data)
    onOpenChange(false)
  }
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-y-auto sm:max-w-[680px]'>
        <DialogHeader>
          <DialogTitle>{t(editData ? 'Edit model' : 'Add model')}</DialogTitle>
          <DialogDescription>
            {t(
              'Configure model pricing. Optional dimensions do not require capability detection.'
            )}
          </DialogDescription>
        </DialogHeader>
        <form
          id={formId}
          className='space-y-6'
          onSubmit={(event) => {
            event.preventDefault()
            save()
          }}
        >
          <div className='space-y-2'>
            <Label htmlFor='pricing-model-name'>{t('Model name')}</Label>
            <Input
              id='pricing-model-name'
              value={state.name}
              required
              disabled={!!editData}
              onChange={(event) =>
                setState({ ...state, name: event.target.value })
              }
            />
          </div>
          <RadioGroup
            value={state.mode}
            onValueChange={(mode) =>
              setState({ ...state, mode: mode as PricingMode })
            }
          >
            {(
              [
                ['per-token', 'Per token'],
                ['per-request', 'Per request'],
                ['tiered_expr', 'Tiered (billing expression)'],
                ...(state.mode === 'audio_seconds'
                  ? [['audio_seconds', 'Audio seconds']]
                  : []),
              ] as const
            ).map(([mode, label]) => (
              <div className='flex items-center gap-2' key={mode}>
                <RadioGroupItem value={mode} id={'pricing-' + mode} />
                <Label htmlFor={'pricing-' + mode}>{t(label)}</Label>
              </div>
            ))}
          </RadioGroup>
          {state.mode === 'per-token' ? (
            <FlatTokenPriceFields value={state.prices} onChange={setPrices} />
          ) : state.mode === 'tiered_expr' ? (
            <TieredPricingEditor
              billingUsage={billingUsageByModel?.[state.name]}
              modelName={state.name}
              billingExpr={state.expression}
              requestRuleExpr={state.request}
              onBillingExprChange={(expression) =>
                setState((s) => ({ ...s, expression }))
              }
              onRequestRuleExprChange={(request) =>
                setState((s) => ({ ...s, request }))
              }
            />
          ) : (
            <div className='space-y-2'>
              <Label htmlFor='pricing-unit-price'>
                {t(
                  state.mode === 'audio_seconds'
                    ? 'Price per 1M audio seconds'
                    : 'Fixed price (USD)'
                )}
              </Label>
              <Input
                id='pricing-unit-price'
                type='number'
                min='0'
                step='any'
                required
                value={
                  state.mode === 'audio_seconds'
                    ? state.prices.inputPrice
                    : state.price
                }
                onChange={(event) =>
                  state.mode === 'audio_seconds'
                    ? setPrices({
                        ...state.prices,
                        inputPrice: event.target.value,
                      })
                    : setState({ ...state, price: event.target.value })
                }
              />
            </div>
          )}
        </form>
        {editData && taskPricing && (
          // Outside the form: plugin buttons must not submit the model price.
          <ModelTaskPluginPricing
            modelName={editData.name}
            plugins={taskPricing.plugins}
            saving={taskPricing.saving}
            lockedReason={taskPricing.lockedReason}
            onSave={taskPricing.onSave}
          />
        )}
        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='submit' form={formId}>
            {t(editData ? 'Update' : 'Add')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
