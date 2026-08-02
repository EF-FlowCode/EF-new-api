import { memo } from 'react'
import { ChevronRight, Copy } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { StatusBadge } from '@/components/status-badge'
import { DEFAULT_TOKEN_UNIT } from '../constants'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import { isTokenBasedModel } from '../lib/model-helpers'
import {
  formatPrice,
  formatRequestPrice,
  formatAudioSecondPrice,
  getEffectivePricingGroup,
} from '../lib/price'
import type { PricingModel, TokenUnit } from '../types'
import { ModelPerfBadge, type ModelPerfBadgeData } from './model-perf-badge'

export interface ModelCardProps {
  model: PricingModel
  onClick: () => void
  priceRate?: number
  usdExchangeRate?: number
  tokenUnit?: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
  perf?: ModelPerfBadgeData
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
  const vendorIcon = props.model.vendor_icon
    ? getLobeIcon(props.model.vendor_icon, 28)
    : null
  const initial = props.model.model_name?.charAt(0).toUpperCase() || '?'
  const isDynamicPricing =
    props.model.billing_mode === 'tiered_expr' &&
    Boolean(props.model.billing_expr)
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
      })
    : null

  const primaryGroup = getEffectivePricingGroup(props.model, selectedGroup)
  const primaryGroupRatio = primaryGroup
    ? props.model.group_ratio?.[primaryGroup]
    : undefined
  const bottomTags = [...endpoints.slice(0, 2), ...tags.slice(0, 2)]
  const hiddenCount =
    Math.max(groups.length - 1, 0) +
    Math.max(endpoints.length - 2, 0) +
    Math.max(tags.length - 2, 0)
  const priceItems = dynamicSummary
    ? dynamicSummary.entries.length > 0
      ? dynamicSummary.entries.map((entry) => ({
          key: entry.key,
          label: t(entry.shortLabel),
          value: entry.formatted,
          unit: `/${tokenUnitLabel}`,
        }))
      : dynamicSummary.tiers.length > 0
        ? dynamicSummary.tiers.map((tier, index) => ({
            key: `tier-${index}`,
            label: tier.label || t('Default'),
            value: '',
            unit: '',
          }))
        : [
            {
              key: 'dynamic',
              label: t('Dynamic Pricing'),
              value: t('Details'),
              unit: '',
            },
          ]
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
            unit: '',
          },
          {
            key: 'audio-output',
            label: t('Output'),
            value: '—',
            unit: '',
          },
        ]
      : isTokenBased
        ? [
            { key: 'input', label: t('Input'), type: 'input' as const },
            { key: 'output', label: t('Output'), type: 'output' as const },
            {
              key: 'create-cache',
              label: t('Cache write'),
              type: 'create_cache' as const,
              unavailable: !hasCreateCachePrice,
            },
            {
              key: 'cache',
              label: t('Cached'),
              type: 'cache' as const,
              unavailable: !hasCachedPrice,
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
              unit: '',
            },
          ]

  const billingTypeLabel = isAudioSeconds
    ? `${t('Audio')} / s`
    : isDynamicPricing
      ? t('Dynamic Pricing')
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
        'group flex flex-col rounded-xl border p-3 transition-colors sm:p-5',
        'hover:bg-muted/20'
      )}
    >
      {/* Header: icon + name + actions */}
      <div className='flex items-start justify-between gap-2.5 sm:gap-3'>
        <div className='flex min-w-0 items-start gap-2.5 sm:gap-3'>
          <div className='bg-muted/40 flex size-9 shrink-0 items-center justify-center rounded-lg sm:size-10 sm:rounded-xl'>
            {vendorIcon || (
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
          <button
            type='button'
            onClick={props.onClick}
            className='text-muted-foreground hover:text-foreground hover:bg-muted inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs transition-colors sm:px-2.5 sm:py-1.5'
          >
            {t('Details')}
            <ChevronRight className='size-3.5' />
          </button>
          <button
            type='button'
            onClick={handleCopy}
            className='text-muted-foreground hover:text-foreground hover:bg-muted rounded-md border p-1.5 transition-colors'
            title={t('Copy')}
          >
            <Copy className='size-3.5' />
          </button>
        </div>
      </div>

      {/* Description */}
      <p className='text-muted-foreground mt-2 line-clamp-1 flex-1 text-[13px] leading-relaxed sm:mt-4 sm:line-clamp-2 sm:min-h-[2.5rem]'>
        {props.model.description || t('No description available.')}
      </p>

      <div className='mt-3 grid grid-cols-2 gap-2'>
        {priceItems.map((item) => (
          <div
            key={item.key}
            className={cn(
              'bg-muted/45 min-w-0 rounded-lg px-3 py-2.5',
              priceItems.length === 1 && 'col-span-2'
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
          </div>
        ))}
      </div>

      {/* Footer row 1: group + billing type */}
      <div className='mt-2 flex flex-wrap items-center gap-x-2 gap-y-1 sm:mt-4'>
        {primaryGroup && (
          <span className='text-muted-foreground text-xs font-medium'>
            {primaryGroup}
            {primaryGroupRatio != null && ` · ${primaryGroupRatio}x`}
          </span>
        )}
        {!isDynamicPricing && (
          <span className='text-muted-foreground text-xs font-medium'>
            {billingTypeLabel}
          </span>
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
      <div className='mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 sm:mt-2 sm:gap-x-3 sm:gap-y-1'>
        {bottomTags.map((item) => (
          <span key={item} className='text-muted-foreground/70 text-xs'>
            {item}
          </span>
        ))}
        <span className='text-muted-foreground/50 text-xs'>
          {tokenUnitLabel}
        </span>
        {hiddenCount > 0 && (
          <span className='text-muted-foreground/40 text-xs'>
            +{hiddenCount}
          </span>
        )}
      </div>
    </div>
  )
})
