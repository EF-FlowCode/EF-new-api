import { Fragment, memo } from 'react'
import { ChevronRight, Clock3, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import { StatusBadge } from '@/components/status-badge'
import { DEFAULT_TOKEN_UNIT } from '../constants'
import { formatTierConditionSummary } from '../lib/billing-expr'
import {
  getDimensionalPricingSummary,
  isDimensionalPricingModel,
} from '../lib/dimensional-price'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
  isDynamicPricingModel,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import {
  getFlatTokenPriceEntries,
  isFlatTokenPriceModel,
} from '../lib/flat-token-price'
import { formatGroupLabel, formatGroupRatio } from '../lib/group-display'
import {
  DEFAULT_PRICING_ENDPOINT,
  formatContextWindow,
  isTokenBasedModel,
  resolveModelEndpointOverride,
  resolveModelIcon,
} from '../lib/model-helpers'
import {
  formatPrice,
  formatRequestPrice,
  formatAudioSecondPrice,
  getEffectivePricingGroup,
} from '../lib/price'
import type { PricingEndpointOverride, PricingModel, TokenUnit } from '../types'
import { ModelPerfBadge, type ModelPerfBadgeData } from './model-perf-badge'

export interface ModelCardProps {
  model: PricingModel
  onClick: () => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
  perf?: ModelPerfBadgeData
  now?: Date
}

export const ModelCard = memo(function ModelCard(props: ModelCardProps) {
  const { t } = useTranslation()
  const { copyToClipboard } = useCopyToClipboard()
  const tokenUnit = props.tokenUnit ?? DEFAULT_TOKEN_UNIT
  const priceRate = props.priceRate ?? 1
  const usdExchangeRate = props.usdExchangeRate ?? 1
  const showRechargePrice = props.showRechargePrice ?? false
  const selectedGroup = props.selectedGroup
  const isTokenBased = isTokenBasedModel(props.model)
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'
  const tags = parseTags(props.model.tags)
  const groups = props.model.enable_groups || []
  const endpoints = props.model.supported_endpoint_types || []
  // 模型元数据的端点是全局维度，无法按分组表达，因此优先显示当前分组
  // 配置的 OpenAI 兼容调用路径。
  const selectedEndpointOverride = resolveModelEndpointOverride(
    props.model,
    props.groupEndpointOverrides,
    selectedGroup
  )
  const displayEndpoints = selectedEndpointOverride?.path
    ? [selectedEndpointOverride.path]
    : endpoints.length > 0
      ? endpoints
      : [DEFAULT_PRICING_ENDPOINT.path]
  const contextLabel = formatContextWindow(props.model.context_window)
  const modelIconKey = resolveModelIcon(props.model)
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 28) : null
  const initial = props.model.model_name?.charAt(0).toUpperCase() || '?'
  const isDynamicPricing = isDynamicPricingModel(props.model)
  const isDimensionalPricing = isDimensionalPricingModel(props.model)
  // eflow: 按音频秒数计费（阿里 ASR）——结算走 ratio，展示成「$X/秒」
  // （formatAudioSecondPrice 已 ÷1e6 取每秒价）。
  const isAudioSeconds = props.model.billing_mode === 'audio_seconds'
  const hasCachedPrice = isTokenBased && props.model.cache_ratio != null
  const hasCreateCachePrice =
    isTokenBased && props.model.create_cache_ratio != null
  const dynamicSummary = isDynamicPricing
    ? getDynamicPricingSummary(props.model, {
        tokenUnit,
        showRechargePrice,
        priceRate,
        usdExchangeRate,
        groupRatioMultiplier: getDynamicDisplayGroupRatio(
          props.model,
          selectedGroup
        ),
        now: props.now,
      })
    : null
  const dimensionalSummary = getDimensionalPricingSummary(props.model, {
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(
      props.model,
      selectedGroup
    ),
  })

  const primaryGroup = getEffectivePricingGroup(props.model, selectedGroup)
  const primaryGroupRatio = primaryGroup
    ? props.model.group_ratio?.[primaryGroup]
    : undefined
  const bottomTags = [...tags.slice(0, 2), ...displayEndpoints.slice(0, 1)]
  const hiddenMetadataCount =
    Math.max(groups.length - 1, 0) +
    Math.max(displayEndpoints.length - 1, 0) +
    Math.max(tags.length - 2, 0)
  const dynamicPreviewRows = (dynamicSummary?.tierRows || [])
    .slice(0, 1)
    .map((row) => ({
      ...row,
      entryMap: new Map(
        row.effectiveEntries.map((entry) => [entry.field, entry])
      ),
    }))
  const dynamicPreviewFields = (dynamicSummary?.priceFields || []).filter(
    (entry) => entry.variable.isBase || entry.variable.group === 'cache'
  )
  const hiddenDynamicTierCount = Math.max(
    (dynamicSummary?.tierRows.length || 0) - dynamicPreviewRows.length,
    0
  )
  const showDynamicTierPreview =
    dynamicPreviewRows.length > 0 && dynamicPreviewFields.length > 0
  const timePricing = dynamicSummary?.timePricing
  const currentTimeMultiplier = timePricing?.currentMultiplier ?? 1
  const currentEffectiveMultiplier =
    dynamicSummary?.currentEffectiveMultiplier ?? currentTimeMultiplier
  const showBaseDynamicPrice = currentTimeMultiplier !== 1
  const effectiveEntryMap = new Map(
    (dynamicSummary?.effectiveEntries || []).map((entry) => [
      entry.field,
      entry,
    ])
  )
  const flatPriceEntries = getFlatTokenPriceEntries(props.model, {
    selectedGroup,
    tokenUnit,
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: getDynamicDisplayGroupRatio(
      props.model,
      selectedGroup
    ),
  })
  const priceItems = dynamicSummary
    ? dynamicSummary.entries.length > 0
      ? dynamicSummary.entries.map((entry) => ({
          key: entry.key,
          label: t(entry.shortLabel),
          value:
            effectiveEntryMap.get(entry.field)?.formatted ?? entry.formatted,
          baseValue: showBaseDynamicPrice ? entry.formatted : undefined,
          unit: `/${tokenUnitLabel}`,
        }))
      : dynamicSummary.tiers.length > 0
        ? dynamicSummary.tiers.map((tier, index) => ({
            key: `tier-${index}`,
            label: tier.label || t('Default'),
            value: '',
            baseValue: undefined,
            unit: '',
          }))
        : [
            {
              key: 'dynamic',
              label: t('Dynamic Pricing'),
              value: t('Details'),
              baseValue: undefined,
              unit: '',
            },
          ]
    : flatPriceEntries.length > 0
      ? flatPriceEntries.map((entry) => ({
          key: entry.key,
          label: t(entry.shortLabel),
          value: entry.formatted,
          baseValue: undefined,
          unit: `/${tokenUnitLabel}`,
        }))
      : isAudioSeconds
        ? [
            {
              key: 'audio-seconds',
              label: `${t('Audio')} / s`,
              value: formatAudioSecondPrice(
                props.model,
                showRechargePrice,
                priceRate,
                usdExchangeRate,
                selectedGroup
              ),
              baseValue: undefined,
              unit: '',
            },
            {
              key: 'audio-output',
              label: t('Output'),
              value: '—',
              baseValue: undefined,
              unit: '',
            },
          ]
        : isTokenBased
          ? [
              { key: 'input', label: t('Input'), type: 'input' as const },
              { key: 'output', label: t('Output'), type: 'output' as const },
              {
                key: 'cache',
                label: t('Cached'),
                type: 'cache' as const,
                unavailable: !hasCachedPrice,
              },
              {
                key: 'create-cache',
                label: t('Cache write'),
                type: 'create_cache' as const,
                unavailable: !hasCreateCachePrice,
              },
            ].map((item) => ({
              key: item.key,
              label: item.label,
              value: item.unavailable
                ? '—'
                : formatPrice(
                    props.model,
                    item.type,
                    tokenUnit,
                    showRechargePrice,
                    priceRate,
                    usdExchangeRate,
                    selectedGroup
                  ),
              baseValue: undefined,
              unit: `/${tokenUnitLabel}`,
            }))
          : [
              {
                key: 'request',
                label: t('Per Request'),
                value: formatRequestPrice(
                  props.model,
                  showRechargePrice,
                  priceRate,
                  usdExchangeRate,
                  selectedGroup
                ),
                baseValue: undefined,
                unit: '',
              },
            ]

  const compactPriceItems = priceItems
  const hiddenCount = hiddenMetadataCount

  const billingTypeLabel = isDimensionalPricing
    ? t('Per second by resolution')
    : isAudioSeconds
      ? `${t('Audio')} / s`
      : isDynamicPricing
        ? t('Dynamic Pricing')
        : isFlatTokenPriceModel(props.model)
          ? t('Fixed token price')
          : isTokenBased
            ? t('Token-based')
            : t('Per Request')

  const handleCopy = (e: React.MouseEvent) => {
    e.stopPropagation()
    copyToClipboard(props.model.model_name || '')
  }

  return (
    <div
      className={cn(
        'group flex h-full flex-col rounded-lg border p-3 transition-colors sm:p-4',
        'hover:bg-muted/20'
      )}
    >
      {/* Header: icon + name + actions */}
      <div className='flex items-start justify-between gap-2.5 sm:gap-3'>
        <div className='flex min-w-0 items-start gap-2.5 sm:gap-3'>
          <div className='bg-muted/40 flex size-9 shrink-0 items-center justify-center rounded-lg sm:size-10'>
            {modelIcon || (
              <span className='text-muted-foreground text-sm font-bold'>
                {initial}
              </span>
            )}
          </div>
          <div className='min-w-0'>
            <h3 className='text-foreground truncate font-mono text-[15px] leading-tight font-bold'>
              {props.model.model_name}
            </h3>
            {props.model.vendor_name && (
              <p className='text-muted-foreground mt-1 truncate text-xs'>
                {props.model.vendor_name}
              </p>
            )}
          </div>
        </div>

        <div className='flex shrink-0 items-center gap-1.5'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={props.onClick}
            className='h-8 px-2.5 text-xs'
          >
            {t('Details')}
            <ChevronRight data-icon='inline-end' />
          </Button>
          <Button
            type='button'
            variant='outline'
            size='icon-sm'
            onClick={handleCopy}
            title={t('Copy')}
            aria-label={t('Copy')}
          >
            <Copy />
          </Button>
        </div>
      </div>

      {/* Description */}
      <p className='text-muted-foreground mt-3 line-clamp-2 min-h-[2.5rem] flex-1 text-[13px] leading-relaxed'>
        {props.model.description || t('No description available.')}
      </p>

      {dimensionalSummary ? (
        <div className='bg-muted/45 mt-3 overflow-hidden rounded-lg'>
          {dimensionalSummary.rows.map((row, index) => (
            <Fragment key={row.key}>
              {index > 0 && <Separator />}
              <div className='flex flex-col gap-2 px-3 py-2'>
                <div className='flex min-w-0 items-center justify-between gap-2'>
                  <span className='text-foreground truncate text-xs font-semibold'>
                    {row.label}
                  </span>
                  <span className='text-muted-foreground truncate text-[10px]'>
                    {t('Billed per second')}
                  </span>
                </div>
                <div className='grid grid-cols-2 gap-x-3 gap-y-1.5'>
                  <div className='flex min-w-0 items-baseline justify-between gap-2'>
                    <span className='text-muted-foreground truncate text-[10px]'>
                      {t('Per second')}
                    </span>
                    <span className='text-foreground shrink-0 font-mono text-xs font-semibold tabular-nums'>
                      {row.formattedUnitPrice}
                    </span>
                  </div>
                  <div className='flex min-w-0 items-baseline justify-between gap-2'>
                    <span className='text-muted-foreground truncate text-[10px]'>
                      {t('{{count}}s reference', {
                        count: dimensionalSummary.sampleQuantity,
                      })}
                    </span>
                    <span className='text-foreground shrink-0 font-mono text-xs font-semibold tabular-nums'>
                      {row.formattedSamplePrice}
                    </span>
                  </div>
                </div>
              </div>
            </Fragment>
          ))}
        </div>
      ) : showDynamicTierPreview ? (
        <div className='bg-muted/45 mt-3 overflow-hidden rounded-lg'>
          {dynamicPreviewRows.map((row, index) => {
            const condition = formatTierConditionSummary(
              row.tier.conditions,
              t,
              row.tier.paramConditions
            )

            return (
              <Fragment key={row.tier.label || index}>
                {index > 0 && <Separator />}
                <div className='flex flex-col gap-2 px-3 py-2'>
                  <div className='flex min-w-0 items-center justify-between gap-2'>
                    <span className='text-foreground truncate text-xs font-semibold'>
                      {row.tier.label || t('Default')}
                    </span>
                    <span className='text-muted-foreground truncate text-[10px]'>
                      {condition || t('Default')}
                    </span>
                  </div>
                  <div className='grid grid-cols-2 gap-x-3 gap-y-1.5'>
                    {dynamicPreviewFields.map((field) => {
                      const entry = row.entryMap.get(field.field)
                      return (
                        <div
                          key={field.field}
                          className='flex min-w-0 items-baseline justify-between gap-2'
                        >
                          <span className='text-muted-foreground truncate text-[10px]'>
                            {t(field.shortLabel)}
                          </span>
                          <span className='text-foreground shrink-0 font-mono text-xs font-semibold tabular-nums'>
                            {entry?.formatted ?? '—'}
                          </span>
                        </div>
                      )
                    })}
                  </div>
                </div>
              </Fragment>
            )
          })}
          {hiddenDynamicTierCount > 0 && (
            <>
              <Separator />
              <div className='text-muted-foreground px-3 py-2 text-center text-[10px]'>
                +
                {t('{{count}} tiers', {
                  count: hiddenDynamicTierCount,
                })}
              </div>
            </>
          )}
        </div>
      ) : (
        <div className='mt-3 grid grid-cols-2 gap-2'>
          {compactPriceItems.map((item, index) => (
            <div
              key={item.key}
              className={cn(
                'bg-muted/45 min-w-0 rounded-lg px-3 py-2',
                (compactPriceItems.length === 1 ||
                  (compactPriceItems.length % 2 === 1 &&
                    index === compactPriceItems.length - 1)) &&
                  'col-span-2'
              )}
            >
              <p className='text-muted-foreground truncate text-[11px]'>
                {item.label}
              </p>
              <p className='text-foreground mt-1 truncate font-mono text-sm font-bold'>
                {item.value}
                {item.unit && (
                  <span className='text-muted-foreground ml-0.5 text-[10px] font-normal'>
                    {item.unit}
                  </span>
                )}
              </p>
              {item.baseValue && (
                <p className='text-muted-foreground mt-0.5 truncate text-[10px]'>
                  {t('Base price: {{price}}', { price: item.baseValue })}
                </p>
              )}
            </div>
          ))}
        </div>
      )}

      {timePricing && (
        <div className='mt-2 flex min-w-0 items-start gap-2'>
          <Clock3 className='text-muted-foreground mt-0.5 size-3.5 shrink-0' />
          <div className='min-w-0 flex-1'>
            <div className='flex flex-wrap items-center gap-1.5'>
              <span className='text-foreground text-xs font-medium'>
                {t('Time-based pricing')}
              </span>
              <Badge
                variant={showBaseDynamicPrice ? 'default' : 'secondary'}
                className='font-mono tabular-nums'
              >
                {t('Current {{multiplier}}x', {
                  multiplier: currentEffectiveMultiplier.toLocaleString(
                    undefined,
                    {
                      maximumFractionDigits: 4,
                    }
                  ),
                })}
              </Badge>
            </div>
            <p className='text-muted-foreground mt-0.5 truncate text-[10px]'>
              {timePricing.periods.length > 0
                ? timePricing.periods
                    .map(
                      (period) =>
                        `${String(period.startHour).padStart(2, '0')}:00-${String(period.endHour).padStart(2, '0')}:00 · ${period.multiplier}x`
                    )
                    .join(', ')
                : t('Custom time rules')}
              {timePricing.timezones.length === 1 &&
                ` · ${timePricing.timezones[0]}`}
            </p>
          </div>
        </div>
      )}

      {/* Footer row 1: group + billing type */}
      <div className='mt-3 flex flex-wrap items-center gap-1.5'>
        {primaryGroup && (
          <Badge
            variant='secondary'
            className='max-w-full gap-1.5'
            title={primaryGroup}
          >
            <span className='truncate'>{formatGroupLabel(primaryGroup)}</span>
            {primaryGroupRatio != null && (
              <span className='font-mono tabular-nums'>
                {formatGroupRatio(primaryGroupRatio)}
              </span>
            )}
          </Badge>
        )}
        {!isDynamicPricing && (
          <Badge variant='outline'>{billingTypeLabel}</Badge>
        )}
        {isDynamicPricing && (
          <StatusBadge
            label={t('Dynamic Pricing')}
            variant='warning'
            copyable={false}
            size='sm'
          />
        )}
        <ModelPerfBadge perf={props.perf} className='ml-auto' />
      </div>

      {/* Footer row 2: endpoint + tag chips */}
      <div className='mt-2 flex flex-wrap items-center gap-1.5'>
        {bottomTags.map((item) => (
          <Badge key={item} variant='outline' className='font-normal'>
            {item}
          </Badge>
        ))}
        {contextLabel && (
          <Badge variant='outline' className='font-normal'>
            {t('Context limit')} {contextLabel}
          </Badge>
        )}
        {hiddenCount > 0 && (
          <Badge variant='outline' className='font-mono font-normal'>
            +{hiddenCount}
          </Badge>
        )}
      </div>
    </div>
  )
})
