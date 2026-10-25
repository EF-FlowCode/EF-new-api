import { useCallback, useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckSquare, RefreshCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  fetchUpstreamRatios,
  getUpstreamChannels,
  updateSystemOptionsBulk,
} from '../api'
import type {
  DifferencesMap,
  RatioType,
  UpstreamChannel,
  UpstreamConfig,
} from '../types'
import { ChannelSelectorDialog } from './channel-selector-dialog'
import {
  ConflictConfirmDialog,
  type ConflictItem,
} from './conflict-confirm-dialog'
import {
  DEFAULT_ENDPOINT,
  MODELS_DEV_PRESET_ENDPOINT,
  MODELS_DEV_PRESET_ID,
  OFFICIAL_CHANNEL_ENDPOINT,
  OFFICIAL_CHANNEL_ID,
  OPENROUTER_CHANNEL_TYPE,
  OPENROUTER_ENDPOINT,
} from './constants'
import {
  buildPricingSyncUpdates,
  getBillingCategory,
  getConfiguredBillingCategory,
  getPreferredSyncField,
  getResolutionBillingCategory,
  type BillingCategory,
  type PricingOptionRecords,
  type ResolutionsMap,
} from './upstream-ratio-sync-helpers'
import { UpstreamRatioSyncTable } from './upstream-ratio-sync-table'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type UpstreamRatioSyncProps = {
  modelRatios: {
    ModelPrice: string
    ModelRatio: string
    CompletionRatio: string
    CacheRatio: string
    CreateCacheRatio: string
    ImageRatio: string
    BatchInputRatio: string
    AudioRatio: string
    AudioCompletionRatio: string
    'billing_setting.billing_mode': string
    'billing_setting.billing_expr': string
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// The two synthesized presets always carry stable negative IDs assigned by
// `controller/ratio_sync.go`; matching by ID alone is sufficient and avoids
// fragile name/base_url comparisons.
function getDefaultEndpointForChannel(channel: UpstreamChannel): string {
  if (channel.id === MODELS_DEV_PRESET_ID) return MODELS_DEV_PRESET_ENDPOINT
  if (channel.id === OFFICIAL_CHANNEL_ID) return OFFICIAL_CHANNEL_ENDPOINT
  if (channel.type === OPENROUTER_CHANNEL_TYPE) return OPENROUTER_ENDPOINT
  return DEFAULT_ENDPOINT
}

function parseJsonRecord<T>(raw: string | undefined | null): Record<string, T> {
  try {
    return JSON.parse(raw || '{}') as Record<string, T>
  } catch {
    return {}
  }
}

function deleteResolutionField(
  res: ResolutionsMap,
  model: string,
  ratioType: string
): ResolutionsMap {
  if (!res[model]) return res
  const newModelRes = { ...res[model] }
  delete newModelRes[ratioType]
  if (ratioType === 'billing_expr') delete newModelRes['billing_mode']
  if (ratioType === 'billing_mode') delete newModelRes['billing_expr']
  const next = { ...res }
  if (Object.keys(newModelRes).length === 0) {
    delete next[model]
  } else {
    next[model] = newModelRes
  }
  return next
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export function UpstreamRatioSync({ modelRatios }: UpstreamRatioSyncProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const [channelDialogOpen, setChannelDialogOpen] = useState(false)
  const [conflictDialogOpen, setConflictDialogOpen] = useState(false)
  const [selectedChannelIds, setSelectedChannelIds] = useState<number[]>([])
  const [channelEndpoints, setChannelEndpoints] = useState<
    Record<number, string>
  >({})
  const [differences, setDifferences] = useState<DifferencesMap>({})
  const [resolutions, setResolutions] = useState<ResolutionsMap>({})
  const [conflictItems, setConflictItems] = useState<ConflictItem[]>([])
  const [confirmLoading, setConfirmLoading] = useState(false)

  const { data: channelsData } = useQuery({
    queryKey: ['upstream-channels'],
    queryFn: getUpstreamChannels,
    enabled: channelDialogOpen,
  })

  // Memoize the channels list so the effect below only re-runs when the query
  // data actually changes, instead of on every render (the `|| []` fallback
  // would otherwise produce a new array reference each render).
  const channels = useMemo(() => channelsData?.data ?? [], [channelsData?.data])

  useEffect(() => {
    if (channels.length === 0) return
    setChannelEndpoints((prev) => {
      let mutated = false
      const next = { ...prev }
      for (const channel of channels) {
        if (!next[channel.id]) {
          next[channel.id] = getDefaultEndpointForChannel(channel)
          mutated = true
        }
      }
      return mutated ? next : prev
    })
  }, [channels])

  const fetchMutation = useMutation({
    mutationFn: fetchUpstreamRatios,
    onSuccess: (data) => {
      if (!data.success) {
        toast.error(data.message || t('Failed to fetch upstream prices'))
        return
      }

      const { differences: diffs, test_results } = data.data

      const errorResults = test_results.filter((r) => r.status === 'error')
      if (errorResults.length > 0) {
        const errorMsg = errorResults
          .map((r) => `${r.name}: ${r.error}`)
          .join(', ')
        toast.warning(t('Some channels failed: {{errorMsg}}', { errorMsg }))
      }

      const successCount = test_results.length - errorResults.length

      setDifferences(diffs)
      setResolutions({})

      if (Object.keys(diffs).length === 0) {
        if (successCount > 0) toast.success(t('No price differences found'))
      } else {
        toast.success(t('Upstream prices fetched successfully'))
      }
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to fetch upstream prices'))
    },
  })

  const { mutate: syncMutate, isPending: isSyncPending } = useMutation({
    mutationFn: (updates: Array<{ key: string; value: string }>) =>
      updateSystemOptionsBulk({ options: updates }),
    onSuccess: () => {
      toast.success(t('Prices synced successfully'))
      queryClient.invalidateQueries({ queryKey: ['system-options'] })

      setDifferences((prevDiffs) => {
        const newDiffs = { ...prevDiffs }
        Object.entries(resolutions).forEach(([model, ratios]) => {
          Object.keys(ratios).forEach((ratioType) => {
            if (newDiffs[model]?.[ratioType as RatioType]) {
              delete newDiffs[model][ratioType as RatioType]
              if (Object.keys(newDiffs[model]).length === 0) {
                delete newDiffs[model]
              }
            }
          })
        })
        return newDiffs
      })

      setResolutions({})
    },
    onError: (error: Error) => {
      toast.error(error.message || t('Failed to sync prices'))
    },
  })

  const handleOpenChannelDialog = () => {
    setChannelDialogOpen(true)
  }

  const handleConfirmChannelSelection = (selectedIds: number[]) => {
    const selectedChannels = channels.filter((ch) =>
      selectedIds.includes(ch.id)
    )

    if (selectedChannels.length === 0) {
      toast.warning(t('Please select at least one channel'))
      return
    }

    const upstreams: UpstreamConfig[] = selectedChannels.map((ch) => ({
      id: ch.id,
      name: ch.name,
      base_url: ch.base_url,
      endpoint: channelEndpoints[ch.id] || DEFAULT_ENDPOINT,
    }))

    fetchMutation.mutate({ upstreams, timeout: 10 })
  }

  const handleSelectValue = useCallback(
    (
      model: string,
      ratioType: RatioType,
      value: number | string,
      sourceName: string
    ) => {
      const modelDiffs = differences[model]

      // Prefer billing_expr over individual ratio fields when available
      const preferredType = sourceName
        ? getPreferredSyncField(modelDiffs || {}, ratioType, sourceName)
        : ratioType
      const preferredValue =
        preferredType === ratioType
          ? value
          : (modelDiffs?.[preferredType]?.upstreams?.[sourceName] ?? value)

      const finalType = preferredType
      const finalValue = preferredValue as number | string
      const category = getBillingCategory(finalType)

      setResolutions((prev) => {
        const newModelRes = { ...(prev[model] || {}) }

        // A model can only select one effective billing category per sync.
        Object.keys(newModelRes).forEach((rt) => {
          if (getBillingCategory(rt) !== category) {
            delete newModelRes[rt]
          }
        })

        newModelRes[finalType] = finalValue

        // Expression billing needs mode and expression from the same source.
        if (category === 'expression' && sourceName && modelDiffs) {
          const modeVal = modelDiffs.billing_mode?.upstreams?.[sourceName]
          const exprVal = modelDiffs.billing_expr?.upstreams?.[sourceName]
          if (modeVal !== undefined && modeVal !== null && modeVal !== 'same') {
            newModelRes['billing_mode'] = modeVal
          } else if (finalType === 'billing_expr') {
            newModelRes['billing_mode'] = 'tiered_expr'
          }
          if (exprVal !== undefined && exprVal !== null && exprVal !== 'same') {
            newModelRes['billing_expr'] = exprVal
          }
        }

        return { ...prev, [model]: newModelRes }
      })
    },
    [differences]
  )

  const handleUnselectValue = useCallback(
    (model: string, ratioType: RatioType) => {
      setResolutions((prev) => deleteResolutionField(prev, model, ratioType))
    },
    []
  )

  const parsedRatios = useMemo(() => {
    return {
      ModelRatio: parseJsonRecord<number>(modelRatios.ModelRatio),
      CompletionRatio: parseJsonRecord<number>(modelRatios.CompletionRatio),
      CacheRatio: parseJsonRecord<number>(modelRatios.CacheRatio),
      CreateCacheRatio: parseJsonRecord<number>(modelRatios.CreateCacheRatio),
      ImageRatio: parseJsonRecord<number>(modelRatios.ImageRatio),
      BatchInputRatio: parseJsonRecord<number>(modelRatios.BatchInputRatio),
      AudioRatio: parseJsonRecord<number>(modelRatios.AudioRatio),
      AudioCompletionRatio: parseJsonRecord<number>(
        modelRatios.AudioCompletionRatio
      ),
      ModelPrice: parseJsonRecord<number>(modelRatios.ModelPrice),
      'billing_setting.billing_mode': parseJsonRecord<string>(
        modelRatios['billing_setting.billing_mode']
      ),
      'billing_setting.billing_expr': parseJsonRecord<string>(
        modelRatios['billing_setting.billing_expr']
      ),
    }
  }, [modelRatios])

  type ParsedRatios = typeof parsedRatios

  const performSync = useCallback(
    async (currentRatios: ParsedRatios): Promise<boolean> => {
      const updates = buildPricingSyncUpdates(
        currentRatios as PricingOptionRecords,
        resolutions
      )

      return new Promise<boolean>((resolve) => {
        syncMutate(updates, {
          onSuccess: () => resolve(true),
          onError: () => resolve(false),
        })
      })
    },
    [resolutions, syncMutate]
  )

  const findSourceChannel = (
    model: string,
    ratioType: RatioType,
    value: number | string
  ): string => {
    const upMap = differences[model]?.[ratioType]?.upstreams
    if (!upMap) return 'Unknown'
    const entry = Object.entries(upMap).find(([, v]) => v === value)
    return entry ? entry[0] : 'Unknown'
  }

  const handleApplySync = () => {
    const currentRatios = parsedRatios
    const conflicts: ConflictItem[] = []

    const fixedPriceLabel = t('Fixed price')
    const modelRatioLabel = t('Model ratio')
    const completionRatioLabel = t('Completion ratio')
    const expressionLabel = t('Expression billing')

    const describeCurrent = (
      category: BillingCategory,
      model: string
    ): string => {
      if (category === 'price') {
        return `${fixedPriceLabel}: ${currentRatios.ModelPrice[model]}`
      }
      if (category === 'expression') {
        const mode =
          currentRatios['billing_setting.billing_mode'][model] ?? 'tiered_expr'
        const expression =
          currentRatios['billing_setting.billing_expr'][model] ?? '-'
        return `${expressionLabel}: ${mode}\n${expression}`
      }
      return `${modelRatioLabel}: ${currentRatios.ModelRatio[model] ?? '-'}\n${completionRatioLabel}: ${currentRatios.CompletionRatio[model] ?? '-'}`
    }

    const describeSelected = (
      category: BillingCategory,
      ratios: Record<string, number | string>
    ): string => {
      if (category === 'price') {
        return `${fixedPriceLabel}: ${ratios.model_price}`
      }
      if (category === 'expression') {
        return `${expressionLabel}: ${ratios.billing_mode ?? 'tiered_expr'}\n${ratios.billing_expr ?? '-'}`
      }
      return `${modelRatioLabel}: ${ratios.model_ratio ?? '-'}\n${completionRatioLabel}: ${ratios.completion_ratio ?? '-'}`
    }

    Object.entries(resolutions).forEach(([model, ratios]) => {
      const localCat = getConfiguredBillingCategory(
        model,
        currentRatios as PricingOptionRecords
      )
      const selectedTypes = Object.keys(ratios)
      const newCat = getResolutionBillingCategory(ratios)

      if (localCat && newCat && localCat !== newCat) {
        const currentDesc = describeCurrent(localCat, model)
        const newDesc = describeSelected(newCat, ratios)

        const channelNames = selectedTypes
          .map((rt) => findSourceChannel(model, rt as RatioType, ratios[rt]))
          .filter((v, idx, arr) => arr.indexOf(v) === idx)
          .join(', ')

        conflicts.push({
          channel: channelNames,
          model,
          current: currentDesc,
          newVal: newDesc,
        })
      }
    })

    if (conflicts.length > 0) {
      setConflictItems(conflicts)
      setConflictDialogOpen(true)
      return
    }

    toast.info(t('Syncing prices, please wait...'))
    performSync(currentRatios)
  }

  const handleConfirmConflict = async () => {
    setConfirmLoading(true)
    try {
      const success = await performSync(parsedRatios)
      if (success) {
        setConflictDialogOpen(false)
      }
    } finally {
      setConfirmLoading(false)
    }
  }

  const hasSelections = Object.keys(resolutions).length > 0
  const isLoading = fetchMutation.isPending || isSyncPending || confirmLoading

  return (
    <div className='space-y-4'>
      <div className='flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
        <div className='flex flex-col gap-2 sm:flex-row'>
          <Button onClick={handleOpenChannelDialog} disabled={isLoading}>
            <RefreshCcw className='mr-2 h-4 w-4' />
            {t('Select Sync Channels')}
          </Button>
          <Button
            variant='secondary'
            onClick={handleApplySync}
            disabled={!hasSelections || isLoading}
          >
            {(isSyncPending || confirmLoading) && (
              <span className='mr-2 h-4 w-4 animate-spin rounded-full border-2 border-current border-t-transparent' />
            )}
            <CheckSquare className='mr-2 h-4 w-4' />
            {t('Apply Sync')}
          </Button>
        </div>
      </div>

      <UpstreamRatioSyncTable
        differences={differences}
        resolutions={resolutions}
        isDisabled={isLoading}
        isSyncing={fetchMutation.isPending}
        onSelectValue={handleSelectValue}
        onUnselectValue={handleUnselectValue}
      />

      <ChannelSelectorDialog
        open={channelDialogOpen}
        onOpenChange={setChannelDialogOpen}
        channels={channels}
        selectedChannelIds={selectedChannelIds}
        onSelectedChannelIdsChange={setSelectedChannelIds}
        channelEndpoints={channelEndpoints}
        onChannelEndpointsChange={setChannelEndpoints}
        onConfirm={handleConfirmChannelSelection}
      />

      <ConflictConfirmDialog
        open={conflictDialogOpen}
        onOpenChange={setConflictDialogOpen}
        conflicts={conflictItems}
        onConfirm={handleConfirmConflict}
        isLoading={confirmLoading}
      />
    </div>
  )
}
