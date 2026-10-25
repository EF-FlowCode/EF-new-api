import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { DataTableColumnHeader } from '@/components/data-table/column-header'
import { GroupBadge } from '@/components/group-badge'
import { DEFAULT_TOKEN_UNIT, QUOTA_TYPE_VALUES } from '../constants'
import { formatTierConditionSummary } from '../lib/billing-expr'
import {
  getDimensionalPricingSummary,
  isDimensionalPricingModel,
} from '../lib/dimensional-price'
import {
  getDynamicDisplayGroupRatio,
  getDynamicPricingSummary,
} from '../lib/dynamic-price'
import { parseTags } from '../lib/filters'
import {
  getFlatTokenPriceEntries,
  isFlatTokenPriceModel,
  parseFlatTokenPriceExpr,
} from '../lib/flat-token-price'
import { isTokenBasedModel, resolveModelIcon } from '../lib/model-helpers'
import {
  formatPrice,
  formatRequestPrice,
  stripTrailingZeros,
} from '../lib/price'
import type { PricingModel, TokenUnit } from '../types'

// ----------------------------------------------------------------------------
// Pricing Table Columns
// ----------------------------------------------------------------------------

// 价格列排序用的标量提取器：token 计费用 model_ratio，按次用 model_price。
// 动态计费（billing_mode === 'tiered_expr'）没有单一标量，返回 Infinity
// 让它在 asc 时垫底、desc 时排前。
function priceSortKey(model: PricingModel): number {
  if (model.knowledge?.prices) return model.knowledge.prices.input
  if (isFlatTokenPriceModel(model)) {
    if (!model.billing_mode || model.billing_mode === 'ratio')
      return model.model_ratio * 2
    const prices = parseFlatTokenPriceExpr(model.billing_expr || '')
    return prices ? Number(prices.inputPrice) : Number.POSITIVE_INFINITY
  }
  if (model.billing_mode === 'tiered_expr') {
    return Number.POSITIVE_INFINITY
  }
  if (model.quota_type === QUOTA_TYPE_VALUES.TOKEN) {
    return model.model_ratio ?? 0
  }
  return model.model_price ?? 0
}

export interface PricingColumnsOptions {
  tokenUnit?: TokenUnit
  priceRate?: number
  usdExchangeRate?: number
  showRechargePrice?: boolean
  selectedGroup?: string
}

function renderLimitedTags(
  items: string[],
  maxDisplay: number = 3
): React.ReactNode {
  if (items.length === 0)
    return <span className='text-muted-foreground/50 text-xs'>—</span>

  const displayed = items.slice(0, maxDisplay)
  const remaining = items.length - maxDisplay

  return (
    <span className='text-muted-foreground text-xs'>
      {displayed.join(', ')}
      {remaining > 0 && (
        <span className='text-muted-foreground/50'> +{remaining}</span>
      )}
    </span>
  )
}

function renderLimitedGroupBadges(
  groups: string[],
  maxDisplay: number = 2
): React.ReactNode {
  if (groups.length === 0)
    return <span className='text-muted-foreground/50 text-xs'>—</span>

  const displayed = groups.slice(0, maxDisplay)
  const remaining = groups.length - maxDisplay

  return (
    <div className='flex max-w-full items-center gap-1 overflow-hidden'>
      {displayed.map((group) => (
        <GroupBadge key={group} group={group} size='sm' />
      ))}
      {remaining > 0 && (
        <span className='text-muted-foreground/50 text-xs'>+{remaining}</span>
      )}
    </div>
  )
}

export function usePricingColumns(
  options: PricingColumnsOptions = {}
): ColumnDef<PricingModel>[] {
  const { t } = useTranslation()
  const {
    tokenUnit = DEFAULT_TOKEN_UNIT,
    priceRate = 1,
    usdExchangeRate = 1,
    showRechargePrice = false,
    selectedGroup,
  } = options

  const tokenUnitLabel = tokenUnit === 'K' ? '1K' : '1M'

  return [
    // Model column
    {
      accessorKey: 'model_name',
      meta: { label: t('Model') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Model')} />
      ),
      cell: ({ row }) => {
        const model = row.original
        const modelIconKey = resolveModelIcon(model)
        const modelIcon = modelIconKey ? getLobeIcon(modelIconKey, 14) : null

        return (
          <div className='flex min-w-[200px] items-center gap-2'>
            {modelIcon}
            <span className='truncate font-mono text-sm font-medium'>
              {model.model_name}
            </span>
          </div>
        )
      },
      minSize: 200,
    },

    // Type column
    {
      accessorKey: 'quota_type',
      meta: { label: t('Type') },
      header: t('Type'),
      cell: ({ row }) => {
        const model = row.original
        const isTokenBased = model.quota_type === QUOTA_TYPE_VALUES.TOKEN
        return (
          <span className='text-muted-foreground text-xs font-medium tracking-wider uppercase'>
            {isDimensionalPricingModel(model)
              ? t('Second')
              : isTokenBased
                ? t('Token')
                : t('Request')}
          </span>
        )
      },
      size: 80,
      enableSorting: false,
    },

    // Price column
    {
      accessorKey: 'price',
      meta: { label: t('Price') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('Price')} />
      ),
      cell: ({ row }) => {
        const model = row.original
        const dimensionalSummary = getDimensionalPricingSummary(model, {
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })
        const dynamicSummary = getDynamicPricingSummary(model, {
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })
        const flatPriceEntries = getFlatTokenPriceEntries(model, {
          selectedGroup,
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })

        if (dimensionalSummary) {
          return (
            <div className='min-w-[190px] space-y-1'>
              {dimensionalSummary.rows.map((priceRow) => (
                <div
                  key={priceRow.key}
                  className='flex items-baseline justify-between gap-3'
                >
                  <span className='text-muted-foreground text-xs font-medium'>
                    {priceRow.label}
                  </span>
                  <span className='font-mono text-sm tabular-nums'>
                    {stripTrailingZeros(priceRow.formattedUnitPrice)}
                    <span className='text-muted-foreground/50 ml-1 text-[10px]'>
                      / {t('Second')}
                    </span>
                  </span>
                </div>
              ))}
            </div>
          )
        }

        if (dynamicSummary) {
          if (dynamicSummary.isSpecialExpression) {
            return (
              <div className='max-w-[320px] min-w-[200px]'>
                <div className='text-xs font-medium text-amber-700 dark:text-amber-300'>
                  {t('Special billing expression')}
                </div>
                <div className='text-muted-foreground text-[11px]'>
                  {t('Unable to parse structured pricing')}
                </div>
                <code className='text-muted-foreground/70 mt-1 line-clamp-2 block font-mono text-[10px] leading-relaxed break-all'>
                  {dynamicSummary.rawExpression}
                </code>
              </div>
            )
          }

          const primaryEntries = dynamicSummary.primaryEntries.slice(0, 2)
          if (primaryEntries.length === 0) {
            return (
              <span className='text-muted-foreground text-xs'>
                {t('Dynamic Pricing')}
              </span>
            )
          }

          if (dynamicSummary.tierRows.length > 1) {
            return (
              <div className='flex min-w-[220px] flex-col gap-1.5'>
                {dynamicSummary.tierRows.slice(0, 2).map((tierRow, index) => {
                  const entryMap = new Map(
                    tierRow.entries.map((entry) => [entry.field, entry])
                  )
                  const input = entryMap.get('inputPrice')
                  const output = entryMap.get('outputPrice')
                  const condition = formatTierConditionSummary(
                    tierRow.tier.conditions,
                    t,
                    tierRow.tier.paramConditions
                  )

                  return (
                    <div
                      key={tierRow.tier.label || index}
                      className='flex items-center justify-between gap-3'
                    >
                      <div className='min-w-0'>
                        <div className='truncate text-xs font-medium'>
                          {tierRow.tier.label || t('Default')}
                        </div>
                        <div className='text-muted-foreground/50 truncate text-[10px]'>
                          {condition || t('Default')}
                        </div>
                      </div>
                      <span className='shrink-0 font-mono text-xs tabular-nums'>
                        {input ? stripTrailingZeros(input.formatted) : '—'}
                        <span className='text-muted-foreground/40 mx-1'>/</span>
                        {output ? stripTrailingZeros(output.formatted) : '—'}
                      </span>
                    </div>
                  )
                })}
                <div className='text-muted-foreground/50 text-[10px]'>
                  {t('Input')} / {t('Output')} · / {tokenUnitLabel} tokens
                  {dynamicSummary.tierRows.length > 2 &&
                    ` · +${t('{{count}} tiers', {
                      count: dynamicSummary.tierRows.length - 2,
                    })}`}
                </div>
              </div>
            )
          }

          return (
            <div className='min-w-[180px]'>
              <span className='font-mono text-sm tabular-nums'>
                {primaryEntries.map((entry, index) => (
                  <span key={entry.key}>
                    {index > 0 && (
                      <span className='text-muted-foreground/40 mx-1'>/</span>
                    )}
                    {stripTrailingZeros(entry.formatted)}
                  </span>
                ))}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                / {tokenUnitLabel} tokens
                {dynamicSummary.tierCount > 1 &&
                  ` · ${t('{{count}} tiers', {
                    count: dynamicSummary.tierCount,
                  })}`}
              </div>
            </div>
          )
        }

        if (flatPriceEntries.length > 0) {
          if (model.knowledge)
            return (
              <div className='min-w-[180px] space-y-1'>
                {flatPriceEntries.map((entry) => (
                  <div
                    key={entry.key}
                    className='flex items-baseline justify-between gap-3'
                  >
                    <span className='text-muted-foreground text-xs'>
                      {t(entry.shortLabel)}
                    </span>
                    <span className='font-mono text-sm tabular-nums'>
                      {entry.formatted}
                      <span className='text-muted-foreground ml-1 text-xs'>
                        / {tokenUnitLabel}
                      </span>
                    </span>
                  </div>
                ))}
              </div>
            )
          const input = flatPriceEntries.find(
            (entry) => entry.field === 'inputPrice'
          )
          const output = flatPriceEntries.find(
            (entry) => entry.field === 'outputPrice'
          )
          return (
            <div className='min-w-[180px]'>
              <span className='font-mono text-sm tabular-nums'>
                {input ? stripTrailingZeros(input.formatted) : '—'}
                <span className='text-muted-foreground/40 mx-1'>/</span>
                {output ? stripTrailingZeros(output.formatted) : '—'}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                {t('Input')} / {t('Output')} · / {tokenUnitLabel} tokens
              </div>
            </div>
          )
        }

        const isTokenBased = isTokenBasedModel(model)

        if (isTokenBased) {
          const inputPrice = stripTrailingZeros(
            formatPrice(
              model,
              'input',
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              selectedGroup
            )
          )
          const outputPrice = stripTrailingZeros(
            formatPrice(
              model,
              'output',
              tokenUnit,
              showRechargePrice,
              priceRate,
              usdExchangeRate,
              selectedGroup
            )
          )

          return (
            <div className='min-w-[160px]'>
              <span className='font-mono text-sm tabular-nums'>
                {inputPrice}
                <span className='text-muted-foreground/40 mx-1'>/</span>
                {outputPrice}
              </span>
              <div className='text-muted-foreground/50 text-[10px]'>
                / {tokenUnitLabel} tokens
              </div>
            </div>
          )
        }

        const price = stripTrailingZeros(
          formatRequestPrice(
            model,
            showRechargePrice,
            priceRate,
            usdExchangeRate,
            selectedGroup
          )
        )

        return (
          <div className='min-w-[100px]'>
            <span className='font-mono text-sm tabular-nums'>{price}</span>
            <div className='text-muted-foreground/50 text-[10px]'>
              / {t('request')}
            </div>
          </div>
        )
      },
      size: 180,
      // 价格列点击排序：cell 是复合展示（input/output 双值或动态计费）不能按
      // accessor 默认字符串比较——自定义 sortingFn 用原始 model_ratio
      // （token 计费）或 model_price（按次计费）做数值排序。dynamic 计费没有
      // 可比标量，统一排到最末。
      sortingFn: (a, b, _columnId) => {
        const aKey = priceSortKey(a.original)
        const bKey = priceSortKey(b.original)
        return aKey - bKey
      },
    },

    // Cached price column (Vercel AI Gateway style)
    {
      id: 'cached_price',
      meta: { label: t('Cached') },
      header: t('Cached'),
      cell: ({ row }) => {
        const model = row.original
        const dynamicSummary = getDynamicPricingSummary(model, {
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        })
        const flatCacheEntries = getFlatTokenPriceEntries(model, {
          tokenUnit,
          showRechargePrice,
          priceRate,
          usdExchangeRate,
          groupRatioMultiplier: getDynamicDisplayGroupRatio(
            model,
            selectedGroup
          ),
        }).filter((entry) => entry.variable.group === 'cache')

        if (dynamicSummary) {
          if (dynamicSummary.isSpecialExpression) {
            return (
              <span className='text-muted-foreground/50 text-xs'>
                {t('Special billing expression')}
              </span>
            )
          }

          const cacheFields = dynamicSummary.priceFields.filter(
            (entry) => entry.variable.group === 'cache'
          )
          if (cacheFields.length === 0) {
            return <span className='text-muted-foreground/30 text-xs'>—</span>
          }

          const tierRows = dynamicSummary.tierRows.slice(
            0,
            dynamicSummary.tierRows.length > 1 ? 2 : 1
          )

          return (
            <div className='flex min-w-[150px] flex-col gap-1.5'>
              {tierRows.map((tierRow, index) => {
                const entryMap = new Map(
                  tierRow.entries.map((entry) => [entry.field, entry])
                )
                return (
                  <div
                    key={tierRow.tier.label || index}
                    className='flex items-center justify-between gap-3'
                  >
                    {dynamicSummary.tierRows.length > 1 && (
                      <span className='text-muted-foreground max-w-24 truncate text-[10px]'>
                        {tierRow.tier.label || t('Default')}
                      </span>
                    )}
                    <span className='ml-auto shrink-0 font-mono text-xs tabular-nums'>
                      {cacheFields.map((field, fieldIndex) => {
                        const entry = entryMap.get(field.field)
                        return (
                          <span key={field.field}>
                            {fieldIndex > 0 && (
                              <span className='text-muted-foreground/40 mx-1'>
                                /
                              </span>
                            )}
                            {entry ? stripTrailingZeros(entry.formatted) : '—'}
                          </span>
                        )
                      })}
                    </span>
                  </div>
                )
              })}
              <div className='text-muted-foreground/50 text-[10px]'>
                {cacheFields.map((field) => t(field.shortLabel)).join(' / ')} ·
                / {tokenUnitLabel}
              </div>
            </div>
          )
        }

        if (flatCacheEntries.length > 0) {
          return (
            <div className='flex min-w-[170px] flex-col gap-1'>
              {flatCacheEntries.map((entry) => (
                <div
                  key={entry.field}
                  className='flex items-baseline justify-between gap-3'
                >
                  <span className='text-muted-foreground text-[10px]'>
                    {t(entry.shortLabel)}
                  </span>
                  <span className='font-mono text-xs tabular-nums'>
                    {stripTrailingZeros(entry.formatted)}
                  </span>
                </div>
              ))}
              <div className='text-muted-foreground/50 text-[10px]'>
                / {tokenUnitLabel}
              </div>
            </div>
          )
        }

        const isTokenBased = isTokenBasedModel(model)

        if (
          !isTokenBased ||
          (model.cache_ratio == null && model.create_cache_ratio == null)
        ) {
          return <span className='text-muted-foreground/30 text-xs'>—</span>
        }

        const cacheEntries = [
          model.cache_ratio != null
            ? {
                key: 'read',
                label: t('Cache Read'),
                value: formatPrice(
                  model,
                  'cache',
                  tokenUnit,
                  showRechargePrice,
                  priceRate,
                  usdExchangeRate,
                  selectedGroup
                ),
              }
            : null,
          model.create_cache_ratio != null
            ? {
                key: 'write',
                label: t('Cache Write'),
                value: formatPrice(
                  model,
                  'create_cache',
                  tokenUnit,
                  showRechargePrice,
                  priceRate,
                  usdExchangeRate,
                  selectedGroup
                ),
              }
            : null,
        ].filter((entry): entry is NonNullable<typeof entry> => entry !== null)

        return (
          <div className='flex min-w-[150px] flex-col gap-1'>
            {cacheEntries.map((entry) => (
              <div
                key={entry.key}
                className='flex items-baseline justify-between gap-3'
              >
                <span className='text-muted-foreground text-[10px]'>
                  {entry.label}
                </span>
                <span className='font-mono text-xs tabular-nums'>
                  {stripTrailingZeros(entry.value)}
                </span>
              </div>
            ))}
            <div className='text-muted-foreground/50 text-[10px]'>
              / {tokenUnitLabel}
            </div>
          </div>
        )
      },
      size: 110,
      enableSorting: false,
    },

    // Vendor column
    {
      accessorKey: 'vendor_name',
      meta: { label: t('Vendor') },
      header: t('Vendor'),
      cell: ({ row }) => {
        const model = row.original
        if (!model.vendor_name) {
          return <span className='text-muted-foreground/50 text-xs'>—</span>
        }
        const vendorIcon = model.vendor_icon
          ? getLobeIcon(model.vendor_icon, 12)
          : null
        return (
          <span className='text-muted-foreground flex items-center gap-1.5 text-xs'>
            {vendorIcon}
            {model.vendor_name}
          </span>
        )
      },
      size: 130,
      enableSorting: false,
    },

    // Tags column
    {
      accessorKey: 'tags',
      meta: { label: t('Tags') },
      header: t('Tags'),
      cell: ({ row }) => {
        const tags = parseTags(row.original.tags)
        if (tags.length === 0) {
          return <span className='text-muted-foreground/50 text-xs'>—</span>
        }

        return (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div>{renderLimitedTags(tags, 2)}</div>
              </TooltipTrigger>
              {tags.length > 2 && (
                <TooltipContent side='top' className='max-w-[280px] p-2'>
                  <span className='text-xs'>{tags.join(', ')}</span>
                </TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
        )
      },
      size: 140,
      enableSorting: false,
    },

    // Endpoints column
    {
      accessorKey: 'supported_endpoint_types',
      meta: { label: t('Endpoints') },
      header: t('Endpoints'),
      cell: ({ row }) => {
        const endpoints = row.original.supported_endpoint_types || []
        if (endpoints.length === 0) {
          return <span className='text-muted-foreground/50 text-xs'>—</span>
        }

        return (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div>{renderLimitedTags(endpoints, 2)}</div>
              </TooltipTrigger>
              {endpoints.length > 2 && (
                <TooltipContent side='top' className='max-w-[280px] p-2'>
                  <span className='text-xs'>{endpoints.join(', ')}</span>
                </TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
        )
      },
      size: 130,
      enableSorting: false,
    },

    // Enable Groups column
    {
      accessorKey: 'enable_groups',
      meta: { label: t('Groups') },
      header: t('Groups'),
      cell: ({ row }) => {
        const groups = row.original.enable_groups || []
        if (groups.length === 0) {
          return <span className='text-muted-foreground/50 text-xs'>—</span>
        }

        return (
          <TooltipProvider>
            <Tooltip>
              <TooltipTrigger asChild>
                <div>{renderLimitedGroupBadges(groups, 2)}</div>
              </TooltipTrigger>
              {groups.length > 2 && (
                <TooltipContent side='top' className='max-w-[280px] p-2'>
                  <div className='flex flex-wrap gap-1'>
                    {groups.map((group) => (
                      <GroupBadge key={group} group={group} size='sm' />
                    ))}
                  </div>
                </TooltipContent>
              )}
            </Tooltip>
          </TooltipProvider>
        )
      },
      size: 130,
      enableSorting: false,
    },
  ]
}
