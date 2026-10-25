import { useMemo } from 'react'
import { useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/ui/markdown'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { CopyButton } from '@/components/copy-button'
import { GroupBadge } from '@/components/group-badge'
import { PageShell } from '@/components/layout'
import { DEFAULT_TOKEN_UNIT, QUOTA_TYPE_VALUES } from '../constants'
import { usePricingData } from '../hooks/use-pricing-data'
import {
  getDimensionalPricingSummary,
  isDimensionalPricingModel,
} from '../lib/dimensional-price'
import {
  getDynamicPriceEntries,
  getDynamicPricingSummary,
  getDynamicPricingTiers,
  isDynamicPricingModel,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import {
  getFlatTokenPriceEntries,
  hasKnowledgeBatchPrice,
  isFlatTokenPriceModel,
} from '../lib/flat-token-price'
import {
  DEFAULT_PRICING_ENDPOINT,
  getAvailableGroups,
  formatContextWindow,
  replaceModelInPath,
  resolveModelEndpointOverride,
  resolveModelIcon,
  isTokenBasedModel,
} from '../lib/model-helpers'
import {
  formatGroupPrice,
  formatFixedPrice,
  formatAudioSecondPrice,
} from '../lib/price'
import type {
  PricingEndpointOverride,
  PricingModel,
  TokenUnit,
  PriceType,
} from '../types'
import { DynamicPricingBreakdown } from './dynamic-pricing-breakdown'
import { ModelDetailsPerformance } from './model-details-performance'

function SectionTitle(props: { children: React.ReactNode }) {
  return (
    <h2 className='text-muted-foreground mb-3 text-xs font-semibold tracking-wider uppercase'>
      {props.children}
    </h2>
  )
}

function ModelHeader(props: { model: PricingModel }) {
  const { t } = useTranslation()
  const model = props.model
  const modelIconKey = resolveModelIcon(model)
  const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 20) : null
  const description = model.description || model.vendor_description || null
  const tags = parseTags(model.tags)
  const contextWindow = formatContextWindow(model.context_window)
  const isFlatPrice = isFlatTokenPriceModel(model)
  const isDynamicPricing = isDynamicPricingModel(model)
  const isSpecialExpression =
    isDynamicPricing && getDynamicPricingTiers(model).length === 0
  const isDimensionalPricing = isDimensionalPricingModel(model)

  return (
    <header className='pb-5'>
      <div className='flex items-center gap-2.5'>
        {modelIcon}
        <h1 className='font-mono text-xl font-bold tracking-tight sm:text-2xl'>
          {model.model_name}
        </h1>
        <CopyButton
          value={model.model_name || ''}
          className='size-6'
          iconClassName='size-3'
          tooltip={t('Copy model name')}
          successTooltip={t('Copied!')}
          aria-label={t('Copy model name')}
        />
      </div>
      <div className='mt-1 flex items-center gap-1.5 text-xs'>
        {model.vendor_name && (
          <span className='text-muted-foreground'>{model.vendor_name}</span>
        )}
        <span className='text-muted-foreground/30'>·</span>
        <span className='text-muted-foreground/50'>
          {isDimensionalPricing
            ? t('Per second by resolution')
            : isFlatPrice
              ? t('Fixed token price')
              : model.quota_type === QUOTA_TYPE_VALUES.TOKEN
                ? t('Token-based')
                : t('Per Request')}
        </span>
        {contextWindow && (
          <>
            <span className='text-muted-foreground/30'>·</span>
            <span className='text-muted-foreground/50'>
              {t('Context limit')} {contextWindow}
            </span>
          </>
        )}
        {isDynamicPricing && model.billing_expr && (
          <>
            <span className='text-muted-foreground/30'>·</span>
            <span className='rounded bg-amber-100 px-1.5 py-0.5 text-[10px] font-medium text-amber-700 dark:bg-amber-500/20 dark:text-amber-300'>
              {isSpecialExpression
                ? t('Special billing expression')
                : t('Dynamic Pricing')}
            </span>
          </>
        )}
      </div>
      {description && (
        <div className='text-muted-foreground mt-2 text-sm leading-relaxed'>
          <Markdown>{description}</Markdown>
        </div>
      )}
      {tags.length > 0 && (
        <div className='mt-2.5 flex flex-wrap gap-1'>
          {tags.map((tag) => (
            <span
              key={tag}
              className='bg-muted text-muted-foreground rounded px-2 py-0.5 text-[11px] font-medium'
            >
              {tag}
            </span>
          ))}
        </div>
      )}
    </header>
  )
}

function PriceSection(props: {
  selectedGroup?: string
  model: PricingModel
  priceRate: number
  usdExchangeRate: number
  tokenUnit: TokenUnit
  showRechargePrice: boolean
}) {
  const { t } = useTranslation()
  const {
    model,
    priceRate,
    usdExchangeRate,
    tokenUnit,
    showRechargePrice,
    selectedGroup,
  } = props
  const isTokenBased = isTokenBasedModel(model)
  // eflow: 按音频秒数计费（阿里 ASR）——结算走 ratio，详情展示「¥X/秒」。
  const isAudioSeconds = model.billing_mode === 'audio_seconds'
  const dimensionalSummary = getDimensionalPricingSummary(model, {
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: 1,
  })
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'
  const baseGroupKey = '_base'
  const baseGroupRatioMap = { [baseGroupKey]: 1 }
  const dynamicSummary = getDynamicPricingSummary(model, {
    tokenUnit,
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: 1,
  })
  const flatPriceEntries = getFlatTokenPriceEntries(model, {
    selectedGroup,
    tokenUnit,
    showRechargePrice,
    priceRate,
    usdExchangeRate,
    groupRatioMultiplier: 1,
  })

  const primaryPriceTypes: { label: string; type: PriceType }[] = [
    { label: t('Input'), type: 'input' },
    { label: t('Output'), type: 'output' },
  ]
  const secondaryPriceTypes: {
    label: string
    type: PriceType
    available: boolean
  }[] = [
    {
      label: t('Cached input'),
      type: 'cache',
      available: model.cache_ratio != null,
    },
    {
      label: t('Cache write'),
      type: 'create_cache',
      available: model.create_cache_ratio != null,
    },
    {
      label: t('Image input'),
      type: 'image',
      available: model.image_ratio != null,
    },
    {
      label: t('Audio input'),
      type: 'audio_input',
      available: model.audio_ratio != null,
    },
    {
      label: t('Audio output'),
      type: 'audio_output',
      available:
        model.audio_ratio != null && model.audio_completion_ratio != null,
    },
  ]

  if (dimensionalSummary) {
    return (
      <section className='border-b py-4'>
        <SectionTitle>{t('Base Price')}</SectionTitle>
        <div className='overflow-hidden rounded-lg border'>
          {dimensionalSummary.rows.map((row, index) => (
            <div
              key={row.key}
              className={index > 0 ? 'border-t px-3 py-3' : 'px-3 py-3'}
            >
              <div className='mb-2 flex items-center justify-between gap-3'>
                <span className='text-sm font-semibold'>{row.label}</span>
                <span className='text-muted-foreground text-xs'>
                  {t('Billed per second')}
                </span>
              </div>
              <div className='grid grid-cols-2 gap-4'>
                <div className='flex items-baseline justify-between gap-2'>
                  <span className='text-muted-foreground text-xs'>
                    {t('Per second')}
                  </span>
                  <span className='font-mono text-sm font-semibold tabular-nums'>
                    {row.formattedUnitPrice}
                  </span>
                </div>
                <div className='flex items-baseline justify-between gap-2'>
                  <span className='text-muted-foreground text-xs'>
                    {t('{{count}}s reference', {
                      count: dimensionalSummary.sampleQuantity,
                    })}
                  </span>
                  <span className='font-mono text-sm font-semibold tabular-nums'>
                    {row.formattedSamplePrice}
                  </span>
                </div>
              </div>
            </div>
          ))}
        </div>
        <p className='text-muted-foreground mt-2 text-xs'>
          {t('Price equals duration times the selected resolution rate.')}
        </p>
      </section>
    )
  }

  if (dynamicSummary) {
    if (dynamicSummary.isSpecialExpression) {
      return (
        <section className='border-b py-4'>
          <SectionTitle>{t('Base Price')}</SectionTitle>
          <div className='rounded-lg border border-amber-200/70 bg-amber-50/70 p-3 dark:border-amber-500/20 dark:bg-amber-500/10'>
            <div className='text-sm font-medium text-amber-800 dark:text-amber-200'>
              {t('Special billing expression')}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Unable to parse structured pricing')}
            </p>
            <div className='mt-3'>
              <div className='text-muted-foreground mb-1 text-[10px] font-medium tracking-wider uppercase'>
                {t('Raw expression')}
              </div>
              <code className='text-muted-foreground bg-background/80 block max-h-28 overflow-auto rounded-md border px-2 py-1.5 font-mono text-xs break-all'>
                {dynamicSummary.rawExpression}
              </code>
            </div>
          </div>
        </section>
      )
    }

    return (
      <section className='border-b py-4'>
        <SectionTitle>{t('Base Price')}</SectionTitle>
        {dynamicSummary.primaryEntries.length > 0 ? (
          <div className='grid grid-cols-2 gap-2'>
            {dynamicSummary.primaryEntries.map((entry) => (
              <div
                key={entry.key}
                className='bg-muted/20 rounded-lg border p-3'
              >
                <div className='text-muted-foreground text-xs'>
                  {t(entry.shortLabel)}
                </div>
                <div className='text-foreground mt-1 font-mono text-base font-semibold tabular-nums'>
                  {entry.formatted}
                  <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
                    / {tokenUnitLabel}
                  </span>
                </div>
              </div>
            ))}
          </div>
        ) : dynamicSummary.tiers.length > 0 ? (
          // eflow: param-driven tiers (no token coefficients) — show each tier
          // as a card so 基础价格 isn't just a "动态计费" placeholder.
          <div className='grid grid-cols-2 gap-2 sm:grid-cols-3'>
            {dynamicSummary.tiers.map((tier, i) => (
              <div
                key={`tier-${i}`}
                className='bg-muted/20 rounded-lg border p-3'
              >
                <div className='text-foreground text-sm leading-snug font-semibold'>
                  {tier.label || t('Default')}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className='text-muted-foreground text-sm'>
            {t('Dynamic Pricing')}
          </p>
        )}
        {dynamicSummary.secondaryEntries.length > 0 && (
          <div className='bg-muted/20 mt-3 rounded-lg border px-3 py-2.5'>
            <div className='space-y-1.5'>
              {dynamicSummary.secondaryEntries.map((entry) => (
                <div
                  key={entry.key}
                  className='flex items-baseline justify-between gap-4'
                >
                  <span className='text-muted-foreground/70 text-sm'>
                    {t(entry.shortLabel)}
                  </span>
                  <span className='text-muted-foreground font-mono text-sm tabular-nums'>
                    {entry.formatted}
                    <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
                      / {tokenUnitLabel}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </section>
    )
  }

  if (flatPriceEntries.length > 0) {
    const primaryEntries = model.knowledge
      ? flatPriceEntries
      : flatPriceEntries.filter((entry) =>
          ['inputPrice', 'outputPrice'].includes(entry.field)
        )
    const cacheEntries = flatPriceEntries.filter(
      (entry) => entry.variable.group === 'cache'
    )
    return (
      <section className='border-b py-4'>
        <SectionTitle>{t('Base Price')}</SectionTitle>
        {model.knowledge?.multimodal && (
          <p className='text-muted-foreground mb-3 text-xs'>
            {t(
              'Image prices apply only when upstream image token counts are available. Unsplit usage uses the synchronous input price.'
            )}
          </p>
        )}
        {model.knowledge && (
          <p className='text-muted-foreground mb-3 text-xs'>
            {t(
              model.knowledge.batch_file
                ? 'Batch File availability depends on the selected group, channel and configured Batch price.'
                : 'This model supports synchronous requests only; native Batch File is not available.'
            )}
          </p>
        )}
        <div className='grid grid-cols-2 gap-2'>
          {primaryEntries.map((entry) => (
            <div
              key={entry.field}
              className='bg-muted/20 rounded-lg border p-3'
            >
              <div className='text-muted-foreground text-xs'>
                {t(entry.shortLabel)}
              </div>
              <div className='text-foreground mt-1 font-mono text-base font-semibold tabular-nums'>
                {entry.formatted}
                <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
                  / {tokenUnitLabel}
                </span>
              </div>
            </div>
          ))}
        </div>
        {cacheEntries.length > 0 && (
          <div className='bg-muted/20 mt-3 rounded-lg border px-3 py-2.5'>
            <div className='flex flex-col gap-1.5'>
              {cacheEntries.map((entry) => (
                <div
                  key={entry.field}
                  className='flex items-baseline justify-between gap-4'
                >
                  <span className='text-muted-foreground/70 text-sm'>
                    {t(entry.shortLabel)}
                  </span>
                  <span className='text-muted-foreground font-mono text-sm tabular-nums'>
                    {entry.formatted}
                    <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
                      / {tokenUnitLabel}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          </div>
        )}
      </section>
    )
  }

  if (isAudioSeconds) {
    return (
      <section className='border-b py-4'>
        <SectionTitle>{t('Base Price')}</SectionTitle>
        <div className='flex items-baseline justify-between'>
          <span className='text-muted-foreground text-sm'>{t('Input')}</span>
          <span className='text-foreground font-mono text-sm font-semibold tabular-nums'>
            {formatAudioSecondPrice(
              model,
              showRechargePrice,
              priceRate,
              usdExchangeRate
            )}
            <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
              /秒
            </span>
          </span>
        </div>
        <div className='text-muted-foreground/60 mt-1 text-xs'>
          {t('Output')} 不计费 · 按音频时长计费
        </div>
      </section>
    )
  }

  if (!isTokenBased) {
    return (
      <section className='border-b py-4'>
        <SectionTitle>{t('Base Price')}</SectionTitle>
        <div className='flex items-baseline justify-between'>
          <span className='text-muted-foreground text-sm'>
            {t('Per request')}
          </span>
          <span className='text-foreground font-mono text-sm font-semibold tabular-nums'>
            {formatFixedPrice(
              model,
              baseGroupKey,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              baseGroupRatioMap
            )}
          </span>
        </div>
      </section>
    )
  }

  const secondaryItems = secondaryPriceTypes.filter((p) => p.available)
  const renderPrice = (type: PriceType) => (
    <>
      {formatGroupPrice(
        model,
        baseGroupKey,
        type,
        tokenUnit,
        showRechargePrice,
        priceRate,
        usdExchangeRate,
        baseGroupRatioMap
      )}
      <span className='text-muted-foreground/40 ml-1 text-xs font-normal'>
        / {tokenUnitLabel}
      </span>
    </>
  )

  return (
    <section className='border-b py-4'>
      <SectionTitle>{t('Base Price')}</SectionTitle>
      <div className='grid grid-cols-2 gap-2'>
        {primaryPriceTypes.map((item) => (
          <div key={item.type} className='bg-muted/20 rounded-lg border p-3'>
            <div className='text-muted-foreground text-xs'>{item.label}</div>
            <div className='text-foreground mt-1 font-mono text-base font-semibold tabular-nums'>
              {renderPrice(item.type)}
            </div>
          </div>
        ))}
      </div>
      {secondaryItems.length > 0 && (
        <div className='bg-muted/20 mt-3 rounded-lg border px-3 py-2.5'>
          <div className='space-y-1.5'>
            {secondaryItems.map((item) => (
              <div
                key={item.type}
                className='flex items-baseline justify-between gap-4'
              >
                <span className='text-muted-foreground/70 text-sm'>
                  {item.label}
                </span>
                <span className='text-muted-foreground font-mono text-sm tabular-nums'>
                  {renderPrice(item.type)}
                </span>
              </div>
            ))}
          </div>
        </div>
      )}
    </section>
  )
}

function EndpointsSection(props: {
  model: PricingModel
  endpointMap: Record<string, { path?: string; method?: string }>
  selectedGroup?: string
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
}) {
  const { t } = useTranslation()
  const { model, endpointMap, selectedGroup, groupEndpointOverrides } = props

  const endpoints = useMemo(() => {
    if (model.knowledge) {
      const embedding = model.knowledge.kind === 'embedding'
      const result = [
        {
          type: embedding ? 'embeddings' : 'rerank',
          path: embedding ? '/v1/embeddings' : '/v1/rerank',
          method: 'POST',
          overridden: false,
        },
      ]
      if (hasKnowledgeBatchPrice(model, selectedGroup)) {
        result.push(
          {
            type: 'files',
            path: '/v1/files',
            method: 'POST',
            overridden: true,
          },
          {
            type: 'batches',
            path: '/v1/batches',
            method: 'POST',
            overridden: true,
          }
        )
      }
      return result
    }
    const override = resolveModelEndpointOverride(
      model,
      groupEndpointOverrides,
      selectedGroup
    )
    if (override?.path) {
      return [
        {
          type: override.path,
          path: override.path,
          method: override.method || 'POST',
          overridden: true,
        },
      ]
    }
    const types = model.supported_endpoint_types || []
    if (types.length === 0) {
      return [
        {
          type: DEFAULT_PRICING_ENDPOINT.type,
          path: DEFAULT_PRICING_ENDPOINT.path,
          method: DEFAULT_PRICING_ENDPOINT.method,
          overridden: true,
        },
      ]
    }
    return types.map((type) => {
      const info = endpointMap[type] || {}
      let path = info.path || ''
      if (path.includes('{model}')) {
        path = replaceModelInPath(path, model.model_name || '')
      }
      return { type, path, method: info.method || 'POST', overridden: false }
    })
  }, [model, endpointMap, selectedGroup, groupEndpointOverrides])

  if (endpoints.length === 0) return null

  return (
    <section className='border-b py-4'>
      <SectionTitle>{t('API Endpoints')}</SectionTitle>
      <div className='space-y-1'>
        {endpoints.map(({ type, path, method, overridden }) => (
          <div key={type} className='flex items-center justify-between py-1'>
            <div className='flex items-center gap-2'>
              {!overridden && (
                <span className='text-sm font-medium'>{type}</span>
              )}
              {path && (
                <code className='text-muted-foreground/60 text-xs break-all'>
                  {path}
                </code>
              )}
            </div>
            {path && (
              <span className='bg-muted text-muted-foreground rounded px-1.5 py-0.5 font-mono text-[10px] font-medium uppercase'>
                {method}
              </span>
            )}
          </div>
        ))}
      </div>
    </section>
  )
}

function AutoGroupChain(props: { model: PricingModel; autoGroups: string[] }) {
  const { t } = useTranslation()
  const modelEnableGroups = Array.isArray(props.model.enable_groups)
    ? props.model.enable_groups
    : []
  const autoChain = props.autoGroups.filter((g) =>
    modelEnableGroups.includes(g)
  )

  if (autoChain.length === 0) return null

  return (
    <div className='text-muted-foreground mb-3 flex flex-wrap items-center gap-1 text-xs'>
      <span className='font-medium'>{t('Auto Group Chain')}</span>
      <span className='text-muted-foreground/40'>→</span>
      {autoChain.map((g, idx) => (
        <span key={g} className='flex items-center gap-1'>
          <GroupBadge group={g} size='sm' />
          {idx < autoChain.length - 1 && (
            <span className='text-muted-foreground/40'>→</span>
          )}
        </span>
      ))}
    </div>
  )
}

function GroupPricingSection(props: {
  model: PricingModel
  groupRatio: Record<string, number>
  usableGroup: Record<string, string>
  autoGroups: string[]
  priceRate: number
  usdExchangeRate: number
  tokenUnit: TokenUnit
  showRechargePrice?: boolean
}) {
  const { t } = useTranslation()
  const {
    model,
    groupRatio,
    usableGroup,
    autoGroups,
    priceRate,
    usdExchangeRate,
    tokenUnit,
    showRechargePrice = false,
  } = props

  const availableGroups = useMemo(
    () => getAvailableGroups(model, usableGroup || {}),
    [model, usableGroup]
  )

  const isTokenBased = isTokenBasedModel(model)
  const isDimensionalPricing = isDimensionalPricingModel(model)
  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'

  const extraPriceTypes = useMemo(() => {
    const types: { label: string; type: PriceType }[] = []
    if (model.cache_ratio != null)
      types.push({ label: t('Cache'), type: 'cache' })
    if (model.create_cache_ratio != null)
      types.push({ label: t('Cache Write'), type: 'create_cache' })
    if (model.image_ratio != null)
      types.push({ label: t('Image'), type: 'image' })
    if (model.audio_ratio != null)
      types.push({ label: t('Audio In'), type: 'audio_input' })
    if (model.audio_ratio != null && model.audio_completion_ratio != null)
      types.push({ label: t('Audio Out'), type: 'audio_output' })
    return types
  }, [model, t])

  if (availableGroups.length === 0) {
    return (
      <section className='py-4'>
        <SectionTitle>{t('Pricing by Group')}</SectionTitle>
        <AutoGroupChain model={model} autoGroups={autoGroups} />
        <p className='text-muted-foreground text-sm'>
          {t(
            'This model is not available in any group, or no group pricing information is configured.'
          )}
        </p>
      </section>
    )
  }

  const thClass =
    'text-muted-foreground py-2 text-[10px] font-medium tracking-wider uppercase'

  if (isDimensionalPricing) {
    return (
      <section className='py-4'>
        <SectionTitle>{t('Pricing by Group')}</SectionTitle>
        <AutoGroupChain model={model} autoGroups={autoGroups} />
        <div className='space-y-3'>
          {availableGroups.map((group) => {
            const ratio = groupRatio[group] ?? 1
            const summary = getDimensionalPricingSummary(model, {
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              groupRatioMultiplier: ratio,
            })
            if (!summary) return null
            return (
              <div key={group} className='overflow-hidden rounded-lg border'>
                <div className='bg-muted/20 flex items-center justify-between gap-3 border-b px-3 py-2'>
                  <GroupBadge group={group} size='sm' />
                  <span className='text-muted-foreground font-mono text-xs'>
                    {ratio}x
                  </span>
                </div>
                <Table className='text-sm'>
                  <TableHeader>
                    <TableRow className='hover:bg-transparent'>
                      <TableHead className={thClass}>
                        {t('Resolution')}
                      </TableHead>
                      <TableHead className={`${thClass} text-right`}>
                        {t('Per second')}
                      </TableHead>
                      <TableHead className={`${thClass} text-right`}>
                        {t('{{count}}s reference', {
                          count: summary.sampleQuantity,
                        })}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {summary.rows.map((row) => (
                      <TableRow key={`${group}-${row.key}`}>
                        <TableCell className='py-2.5 text-xs font-medium'>
                          {row.label}
                        </TableCell>
                        <TableCell className='py-2.5 text-right font-mono tabular-nums'>
                          {row.formattedUnitPrice}
                        </TableCell>
                        <TableCell className='py-2.5 text-right font-mono tabular-nums'>
                          {row.formattedSamplePrice}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )
          })}
        </div>
      </section>
    )
  }

  if (isFlatTokenPriceModel(model)) {
    return (
      <section className='py-4'>
        <SectionTitle>{t('Pricing by Group')}</SectionTitle>
        <AutoGroupChain model={model} autoGroups={autoGroups} />
        <div className='flex flex-col gap-3'>
          {availableGroups.map((group) => {
            const ratio = groupRatio[group] ?? 1
            const entries = getFlatTokenPriceEntries(model, {
              selectedGroup: group,
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              groupRatioMultiplier: ratio,
            })
            return (
              <div key={group} className='overflow-hidden rounded-lg border'>
                <div className='bg-muted/20 flex items-center justify-between gap-3 border-b px-3 py-2'>
                  <GroupBadge group={group} size='sm' />
                  <span className='text-muted-foreground font-mono text-xs'>
                    {ratio}x
                  </span>
                </div>
                <div className='flex flex-col gap-1.5 px-3 py-2.5'>
                  {entries.map((entry) => (
                    <div
                      key={entry.field}
                      className='flex items-baseline justify-between gap-4'
                    >
                      <span className='text-muted-foreground text-xs'>
                        {t(entry.shortLabel)}
                      </span>
                      <span className='font-mono text-sm tabular-nums'>
                        {entry.formatted}
                        <span className='text-muted-foreground/40 ml-1 text-xs'>
                          / {tokenUnitLabel}
                        </span>
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )
          })}
        </div>
      </section>
    )
  }

  if (isDynamicPricingModel(model)) {
    const dynamicTiers = getDynamicPricingTiers(model)

    if (dynamicTiers.length === 0) {
      return (
        <section className='py-4'>
          <SectionTitle>{t('Pricing by Group')}</SectionTitle>
          <AutoGroupChain model={model} autoGroups={autoGroups} />
          <div className='rounded-lg border border-amber-200/70 bg-amber-50/70 p-3 dark:border-amber-500/20 dark:bg-amber-500/10'>
            <div className='text-sm font-medium text-amber-800 dark:text-amber-200'>
              {t('Special billing expression')}
            </div>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Group prices cannot be expanded because this expression is not a standard tiered pricing expression.'
              )}
            </p>
            <div className='mt-3'>
              <div className='text-muted-foreground mb-1 text-[10px] font-medium tracking-wider uppercase'>
                {t('Raw expression')}
              </div>
              <code className='text-muted-foreground bg-background/80 block max-h-28 overflow-auto rounded-md border px-2 py-1.5 font-mono text-xs break-all'>
                {model.billing_expr}
              </code>
            </div>
          </div>
        </section>
      )
    }

    const priceFields = Array.from(
      new Map(
        dynamicTiers
          .flatMap((tier) =>
            getDynamicPriceEntries(tier, {
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              groupRatioMultiplier: 1,
            })
          )
          .map((entry) => [entry.field, entry])
      ).values()
    )

    return (
      <section className='py-4'>
        <SectionTitle>{t('Pricing by Group')}</SectionTitle>
        <AutoGroupChain model={model} autoGroups={autoGroups} />
        <div className='space-y-3'>
          {availableGroups.map((group) => {
            const ratio = groupRatio[group] ?? 1
            return (
              <div key={group} className='overflow-hidden rounded-lg border'>
                <div className='bg-muted/20 flex items-center justify-between gap-3 border-b px-3 py-2'>
                  <GroupBadge group={group} size='sm' />
                  <span className='text-muted-foreground font-mono text-xs'>
                    {ratio}x
                  </span>
                </div>
                <div className='overflow-x-auto'>
                  <Table className='text-sm'>
                    <TableHeader>
                      <TableRow className='hover:bg-transparent'>
                        <TableHead className={thClass}>{t('Tier')}</TableHead>
                        {priceFields.map((entry) => (
                          <TableHead
                            key={entry.field}
                            className={`${thClass} text-right`}
                          >
                            {t(entry.shortLabel)}
                          </TableHead>
                        ))}
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {dynamicTiers.map((tier, tierIndex) => {
                        const entries = getDynamicPriceEntries(tier, {
                          tokenUnit,
                          showRechargePrice,
                          priceRate,
                          usdExchangeRate,
                          groupRatioMultiplier: ratio,
                        })
                        const entryMap = new Map(
                          entries.map((entry) => [entry.field, entry])
                        )

                        return (
                          <TableRow key={`${group}-${tier.label || tierIndex}`}>
                            <TableCell className='text-muted-foreground py-2.5 text-xs'>
                              {tier.label || t('Default')}
                            </TableCell>
                            {priceFields.map((fieldEntry) => {
                              const entry = entryMap.get(fieldEntry.field)
                              return (
                                <TableCell
                                  key={fieldEntry.field}
                                  className='py-2.5 text-right font-mono'
                                >
                                  {entry?.formatted ?? '-'}
                                </TableCell>
                              )
                            })}
                          </TableRow>
                        )
                      })}
                    </TableBody>
                  </Table>
                </div>
              </div>
            )
          })}
          <p className='text-muted-foreground/40 mt-1.5 px-4 text-[10px] sm:px-0'>
            {t('Prices shown per')} {tokenUnitLabel} tokens
          </p>
        </div>
      </section>
    )
  }

  return (
    <section className='py-4'>
      <SectionTitle>{t('Pricing by Group')}</SectionTitle>
      <AutoGroupChain model={model} autoGroups={autoGroups} />
      <div className='-mx-4 sm:mx-0'>
        <Table className='text-sm'>
          <TableHeader>
            <TableRow className='hover:bg-transparent'>
              <TableHead className={thClass}>{t('Group')}</TableHead>
              <TableHead className={thClass}>{t('Ratio')}</TableHead>
              {isTokenBased ? (
                <>
                  <TableHead className={`${thClass} text-right`}>
                    {t('Input')}
                  </TableHead>
                  <TableHead className={`${thClass} text-right`}>
                    {t('Output')}
                  </TableHead>
                  {extraPriceTypes.map((ep) => (
                    <TableHead
                      key={ep.type}
                      className={`${thClass} text-right`}
                    >
                      {ep.label}
                    </TableHead>
                  ))}
                </>
              ) : (
                <TableHead className={`${thClass} text-right`}>
                  {t('Price')}
                </TableHead>
              )}
            </TableRow>
          </TableHeader>
          <TableBody>
            {availableGroups.map((group) => {
              const ratio = groupRatio[group] ?? 1
              return (
                <TableRow key={group}>
                  <TableCell className='py-2.5'>
                    <GroupBadge group={group} size='sm' />
                  </TableCell>
                  <TableCell className='text-muted-foreground py-2.5 font-mono text-xs'>
                    {ratio}x
                  </TableCell>
                  {isTokenBased ? (
                    <>
                      <TableCell className='py-2.5 text-right font-mono'>
                        {formatGroupPrice(
                          model,
                          group,
                          'input',
                          tokenUnit,
                          showRechargePrice,
                          priceRate,
                          usdExchangeRate,
                          groupRatio
                        )}
                      </TableCell>
                      <TableCell className='py-2.5 text-right font-mono'>
                        {formatGroupPrice(
                          model,
                          group,
                          'output',
                          tokenUnit,
                          showRechargePrice,
                          priceRate,
                          usdExchangeRate,
                          groupRatio
                        )}
                      </TableCell>
                      {extraPriceTypes.map((ep) => (
                        <TableCell
                          key={ep.type}
                          className='py-2.5 text-right font-mono'
                        >
                          {formatGroupPrice(
                            model,
                            group,
                            ep.type,
                            tokenUnit,
                            showRechargePrice,
                            priceRate,
                            usdExchangeRate,
                            groupRatio
                          )}
                        </TableCell>
                      ))}
                    </>
                  ) : (
                    <TableCell className='py-2.5 text-right font-mono'>
                      {formatFixedPrice(
                        model,
                        group,
                        showRechargePrice,
                        priceRate,
                        usdExchangeRate,
                        groupRatio
                      )}
                    </TableCell>
                  )}
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        {isTokenBased && (
          <p className='text-muted-foreground/40 mt-1.5 px-4 text-[10px] sm:px-0'>
            {t('Prices shown per')} {tokenUnitLabel} tokens
          </p>
        )}
      </div>
    </section>
  )
}

export interface ModelDetailsContentProps {
  model: PricingModel
  groupRatio: Record<string, number>
  usableGroup: Record<string, string>
  endpointMap: Record<string, { path?: string; method?: string }>
  autoGroups: string[]
  priceRate: number
  usdExchangeRate: number
  tokenUnit: TokenUnit
  showRechargePrice?: boolean
  selectedGroup?: string
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
}

export function ModelDetailsContent(props: ModelDetailsContentProps) {
  const { t } = useTranslation()
  const {
    model,
    groupRatio,
    usableGroup,
    endpointMap,
    autoGroups,
    priceRate,
    usdExchangeRate,
    tokenUnit,
    showRechargePrice = false,
    selectedGroup,
    groupEndpointOverrides,
  } = props

  return (
    <>
      <ModelHeader model={model} />

      <PriceSection
        selectedGroup={selectedGroup}
        model={model}
        priceRate={priceRate}
        usdExchangeRate={usdExchangeRate}
        tokenUnit={tokenUnit}
        showRechargePrice={showRechargePrice}
      />

      <EndpointsSection
        model={model}
        endpointMap={endpointMap}
        selectedGroup={selectedGroup}
        groupEndpointOverrides={groupEndpointOverrides}
      />

      {isDynamicPricingModel(model) && model.billing_expr && (
        <div className='border-b'>
          <DynamicPricingBreakdown billingExpr={model.billing_expr} />
        </div>
      )}

      <GroupPricingSection
        model={model}
        groupRatio={groupRatio}
        usableGroup={usableGroup}
        autoGroups={autoGroups}
        priceRate={priceRate}
        usdExchangeRate={usdExchangeRate}
        tokenUnit={tokenUnit}
        showRechargePrice={showRechargePrice}
      />

      <section className='py-4'>
        <SectionTitle>{t('Performance')}</SectionTitle>
        <ModelDetailsPerformance model={model} />
      </section>
    </>
  )
}

export interface ModelDetailsDrawerProps extends ModelDetailsContentProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function ModelDetailsDrawer(props: ModelDetailsDrawerProps) {
  const { t } = useTranslation()
  const { open, onOpenChange, ...contentProps } = props

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side='right'
        className='flex h-dvh w-full overflow-hidden p-0 sm:max-w-2xl xl:max-w-3xl'
      >
        <SheetHeader className='sr-only'>
          <SheetTitle>{props.model.model_name}</SheetTitle>
          <SheetDescription>{t('Model details')}</SheetDescription>
        </SheetHeader>
        <div className='flex-1 overflow-y-auto px-4 pt-11 pb-5 sm:px-6 sm:pt-12 sm:pb-6'>
          <ModelDetailsContent {...contentProps} />
        </div>
      </SheetContent>
    </Sheet>
  )
}

export function ModelDetails() {
  const { t } = useTranslation()
  const { modelId } = useParams({ from: '/_authenticated/pricing/$modelId/' })
  const search = useSearch({ from: '/_authenticated/pricing/$modelId/' })
  const navigate = useNavigate()

  const {
    models,
    groupRatio,
    usableGroup,
    endpointMap,
    groupEndpointOverrides,
    autoGroups,
    isLoading,
    priceRate,
    usdExchangeRate,
  } = usePricingData()

  const tokenUnit: TokenUnit =
    search.tokenUnit === 'K' ? 'K' : DEFAULT_TOKEN_UNIT

  const model = useMemo(() => {
    if (!models || !modelId) return null
    return models.find((m) => m.model_name === modelId) || null
  }, [models, modelId])

  const handleBack = () => {
    navigate({ to: '/pricing', search })
  }

  if (isLoading) {
    return (
      <PageShell>
        <div className='mx-auto max-w-2xl px-4 sm:px-6'>
          <Skeleton className='mb-4 h-5 w-16' />
          <div className='space-y-2'>
            <Skeleton className='h-7 w-64' />
            <Skeleton className='h-4 w-40' />
            <Skeleton className='h-4 w-full max-w-md' />
          </div>
          <div className='mt-6 space-y-3'>
            {Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className='flex justify-between'>
                <Skeleton className='h-5 w-24' />
                <Skeleton className='h-5 w-20' />
              </div>
            ))}
          </div>
        </div>
      </PageShell>
    )
  }

  if (!model) {
    return (
      <PageShell>
        <div className='mx-auto max-w-2xl px-4 text-center sm:px-6'>
          <h2 className='mb-1 text-base font-semibold'>
            {t('Model not found')}
          </h2>
          <p className='text-muted-foreground mb-4 text-sm'>
            {t("The model you're looking for doesn't exist.")}
          </p>
          <Button onClick={handleBack} variant='outline' size='sm'>
            {t('Back to Models')}
          </Button>
        </div>
      </PageShell>
    )
  }

  return (
    <PageShell>
      <div className='mx-auto max-w-2xl px-4 sm:px-6'>
        <Button
          variant='ghost'
          size='sm'
          onClick={handleBack}
          className='text-muted-foreground hover:text-foreground mb-4 h-auto gap-1 px-0 py-1 text-xs'
        >
          <ArrowLeft className='size-3.5' />
          {t('Back')}
        </Button>

        <ModelDetailsContent
          model={model}
          groupRatio={groupRatio || {}}
          usableGroup={usableGroup || {}}
          autoGroups={autoGroups || []}
          priceRate={priceRate ?? 1}
          usdExchangeRate={usdExchangeRate ?? 1}
          tokenUnit={tokenUnit}
          showRechargePrice={search.rechargePrice ?? false}
          endpointMap={
            (endpointMap as Record<
              string,
              { path?: string; method?: string }
            >) || {}
          }
          selectedGroup={search.group}
          groupEndpointOverrides={groupEndpointOverrides}
        />
      </div>
    </PageShell>
  )
}
