import { useCallback, useMemo, useState } from 'react'
import { ArrowUpDown, Check, Filter } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { FILTER_ALL, getSortLabels, type SortOption } from '../constants'
import { parseTags } from '../lib/filters'
import {
  formatGroupLabel,
  formatGroupRatio,
  getGroupLayoutRow,
} from '../lib/group-display'
import type {
  PricingEndpointOverride,
  PricingModel,
  PricingVendor,
  TokenUnit,
} from '../types'
import { PricingSidebar } from './pricing-sidebar'

type SegmentOption = {
  value: string
  label?: string
  icon?: React.ComponentType<{ className?: string }>
  tooltip?: string
}

const PREFERRED_TAGS = [
  '推理',
  '代码',
  '多模态',
  '工具',
  '语音识别',
  '按时长计费',
  'embedding',
  'rerank',
]

export interface PricingToolbarProps {
  filteredCount: number
  totalCount?: number
  sortBy: string
  onSortChange: (value: string) => void
  tokenUnit: TokenUnit
  onTokenUnitChange: (value: TokenUnit) => void
  showRechargePrice: boolean
  onRechargePriceChange: (value: boolean) => void
  quotaTypeFilter: string
  endpointTypeFilter: string
  vendorFilter: string
  groupFilter: string
  tagFilter: string
  onQuotaTypeChange: (value: string) => void
  onEndpointTypeChange: (value: string) => void
  onVendorChange: (value: string) => void
  onGroupChange: (value: string) => void
  onTagChange: (value: string) => void
  vendors: PricingVendor[]
  groups: string[]
  groupRatios?: Record<string, number>
  tags: string[]
  models: PricingModel[]
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
  hasActiveFilters: boolean
  activeFilterCount: number
  onClearFilters: () => void
}

function SegmentedControl(props: {
  options: SegmentOption[]
  value: string
  onChange: (value: string) => void
  ariaLabel: string
}) {
  return (
    <div
      role='group'
      aria-label={props.ariaLabel}
      className='bg-muted/60 inline-flex h-8 items-center rounded-md border p-0.5'
    >
      {props.options.map((option) => {
        const Icon = option.icon
        const isActive = option.value === props.value
        const button = (
          <button
            key={option.value}
            type='button'
            onClick={() => props.onChange(option.value)}
            aria-pressed={isActive}
            className={cn(
              'inline-flex h-full items-center justify-center rounded-[5px] text-xs font-medium transition-all',
              Icon && !option.label ? 'w-7' : 'gap-1.5 px-3',
              isActive
                ? 'bg-foreground text-background shadow-sm'
                : 'text-muted-foreground hover:text-foreground'
            )}
          >
            {Icon && <Icon className='size-3.5' />}
            {option.label}
          </button>
        )

        if (!option.tooltip) {
          return button
        }

        return (
          <Tooltip key={option.value}>
            <TooltipTrigger asChild>{button}</TooltipTrigger>
            <TooltipContent side='bottom' className='text-xs'>
              {option.tooltip}
            </TooltipContent>
          </Tooltip>
        )
      })}
    </div>
  )
}

export function PricingToolbar(props: PricingToolbarProps) {
  const { t } = useTranslation()
  const [mobileFiltersOpen, setMobileFiltersOpen] = useState(false)
  const sortLabels = getSortLabels(t)

  const vendors = useMemo(() => {
    return props.vendors
      .map((vendor) => ({
        ...vendor,
        count: props.models.filter((model) => model.vendor_name === vendor.name)
          .length,
      }))
      .filter((vendor) => vendor.count > 0)
      .sort((a, b) => b.count - a.count || a.name.localeCompare(b.name))
  }, [props.models, props.vendors])

  const tags = useMemo(() => {
    const tagCounts = new Map<string, number>()
    for (const model of props.models) {
      for (const tag of parseTags(model.tags)) {
        const normalized = tag.toLowerCase()
        tagCounts.set(normalized, (tagCounts.get(normalized) || 0) + 1)
      }
    }

    const availableTags = new Map(
      props.tags.map((tag) => [tag.toLowerCase(), tag])
    )
    const preferred = PREFERRED_TAGS.map((tag) =>
      availableTags.get(tag.toLowerCase())
    ).filter((tag): tag is string => Boolean(tag))
    const fallback = props.tags
      .filter((tag) => !preferred.includes(tag))
      .sort(
        (a, b) =>
          (tagCounts.get(b.toLowerCase()) || 0) -
            (tagCounts.get(a.toLowerCase()) || 0) || a.localeCompare(b)
      )
    return [...preferred, ...fallback].map((tag) => ({
      value: tag,
      count: tagCounts.get(tag.toLowerCase()) || 0,
    }))
  }, [props.models, props.tags])

  const groupRows = useMemo(() => {
    const countByGroup = new Map<string, number>()
    for (const model of props.models) {
      for (const group of model.enable_groups || []) {
        countByGroup.set(group, (countByGroup.get(group) || 0) + 1)
      }
    }

    const groups = props.groups
      .map((group) => ({
        value: group,
        label: formatGroupLabel(group),
        count: countByGroup.get(group) || 0,
        ratio: formatGroupRatio(props.groupRatios?.[group]),
      }))
      .filter((group) => group.count > 0 || group.value === props.groupFilter)
    const rows = new Map<number | undefined, typeof groups>()

    for (const group of groups) {
      const row = getGroupLayoutRow(group.value)
      const existing = rows.get(row)
      if (existing) existing.push(group)
      else rows.set(row, [group])
    }

    return Array.from(rows, ([row, items]) => ({
      row,
      items: items.sort((a, b) => a.value.localeCompare(b.value)),
    })).sort((a, b) => {
      if (a.row == null) return 1
      if (b.row == null) return -1
      return a.row - b.row
    })
  }, [props.groupFilter, props.groupRatios, props.groups, props.models])

  const handleTokenUnitChange = useCallback(
    (value: string) => props.onTokenUnitChange(value as TokenUnit),
    [props]
  )

  const handleRechargePriceChange = useCallback(
    (value: string) => props.onRechargePriceChange(value === 'recharge'),
    [props]
  )

  return (
    <div className='flex flex-col gap-3 rounded-lg border p-3'>
      <div className='flex flex-col gap-2 border-b pb-3'>
        <div className='grid min-w-0 grid-cols-[5.5rem_minmax(0,1fr)] items-start gap-2'>
          <span className='text-muted-foreground pt-1.5 text-xs font-medium'>
            {t('All Vendors')}
          </span>
          <div className='min-w-0 pb-1'>
            <div className='flex min-w-0 flex-wrap items-center gap-1'>
              <Button
                type='button'
                variant={
                  props.vendorFilter === FILTER_ALL ? 'secondary' : 'ghost'
                }
                size='sm'
                onClick={() => props.onVendorChange(FILTER_ALL)}
                className='h-7 px-2.5 text-xs'
              >
                {t('All')}
              </Button>
              {vendors.map((vendor) => (
                <Button
                  key={vendor.id || vendor.name}
                  type='button'
                  variant={
                    props.vendorFilter === vendor.name ? 'secondary' : 'ghost'
                  }
                  size='sm'
                  onClick={() =>
                    props.onVendorChange(
                      props.vendorFilter === vendor.name
                        ? FILTER_ALL
                        : vendor.name
                    )
                  }
                  className='h-auto min-h-7 max-w-full px-2.5 py-1 text-xs whitespace-normal'
                  title={vendor.name}
                >
                  {vendor.icon && getLobeIcon(vendor.icon, 14)}
                  <span className='min-w-0 text-left wrap-anywhere'>
                    {vendor.name}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {vendor.count}
                  </span>
                </Button>
              ))}
            </div>
          </div>
        </div>

        <div className='grid min-w-0 grid-cols-[5.5rem_minmax(0,1fr)] items-start gap-2'>
          <span className='text-muted-foreground pt-1.5 text-xs font-medium'>
            {t('Model Tags')}
          </span>
          <div className='min-w-0 pb-1'>
            <div className='flex min-w-0 flex-wrap items-center gap-1'>
              {tags.map((tag) => (
                <Button
                  key={tag.value}
                  type='button'
                  variant={
                    props.tagFilter === tag.value ? 'secondary' : 'ghost'
                  }
                  size='sm'
                  onClick={() =>
                    props.onTagChange(
                      props.tagFilter === tag.value ? FILTER_ALL : tag.value
                    )
                  }
                  className='h-auto min-h-7 max-w-full px-2.5 py-1 text-xs whitespace-normal'
                >
                  <span className='min-w-0 text-left wrap-anywhere'>
                    {tag.value}
                  </span>
                  <span className='text-muted-foreground tabular-nums'>
                    {tag.count}
                  </span>
                </Button>
              ))}
            </div>
          </div>
        </div>

        <div className='grid min-w-0 grid-cols-[5.5rem_minmax(0,1fr)] items-start gap-2'>
          <span className='text-muted-foreground pt-1.5 text-xs font-medium'>
            {t('Groups')}
          </span>
          <div className='flex min-w-0 flex-col gap-1.5'>
            <div className='grid min-w-0 grid-cols-[2.5rem_minmax(0,1fr)] items-center gap-1.5'>
              <span className='text-muted-foreground font-mono text-[10px] font-semibold'>
                AUTO
              </span>
              <div className='min-w-0 overflow-x-auto pb-0.5'>
                <div className='flex w-max min-w-full items-center gap-1 sm:w-full sm:flex-wrap'>
                  <Button
                    type='button'
                    variant={
                      props.groupFilter === FILTER_ALL ? 'secondary' : 'ghost'
                    }
                    size='sm'
                    onClick={() => props.onGroupChange(FILTER_ALL)}
                    className='h-7 px-2.5 text-xs'
                  >
                    {t('Auto')}
                    <span className='text-muted-foreground'>
                      {t('Lowest price')}
                    </span>
                  </Button>
                </div>
              </div>
            </div>

            {groupRows.map((groupRow) => (
              <div
                key={groupRow.row ?? 'other'}
                className='grid min-w-0 grid-cols-[2.5rem_minmax(0,1fr)] items-start gap-1.5'
              >
                <span className='text-muted-foreground pt-1.5 font-mono text-[10px] font-semibold'>
                  {groupRow.row == null ? t('Other') : `${groupRow.row}_`}
                </span>
                <div className='min-w-0 overflow-x-auto pb-0.5'>
                  <div className='flex w-max min-w-full items-center gap-1 sm:w-full sm:flex-wrap'>
                    {groupRow.items.map((group) => (
                      <Button
                        key={group.value}
                        type='button'
                        variant={
                          props.groupFilter === group.value
                            ? 'secondary'
                            : 'ghost'
                        }
                        size='sm'
                        onClick={() =>
                          props.onGroupChange(
                            props.groupFilter === group.value
                              ? FILTER_ALL
                              : group.value
                          )
                        }
                        className='h-7 px-2.5 text-xs'
                        title={group.value}
                      >
                        <span>{group.label}</span>
                        <span className='text-muted-foreground tabular-nums'>
                          {group.count}
                        </span>
                        {group.ratio && (
                          <Badge
                            variant='outline'
                            className='px-1.5 py-0 font-mono text-[10px]'
                          >
                            {group.ratio}
                          </Badge>
                        )}
                      </Button>
                    ))}
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>

      <div className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => setMobileFiltersOpen(true)}
          >
            <Filter data-icon='inline-start' />
            {t('Filter')}
            {props.activeFilterCount > 0 && (
              <Badge className='ml-0.5 size-5 justify-center rounded-full p-0 text-[10px]'>
                {props.activeFilterCount}
              </Badge>
            )}
          </Button>

          <div className='text-muted-foreground flex items-baseline gap-1 text-sm'>
            <span className='text-foreground font-semibold tabular-nums'>
              {props.filteredCount.toLocaleString()}
            </span>
            <span>{props.filteredCount === 1 ? t('model') : t('models')}</span>
            {props.hasActiveFilters && props.totalCount && (
              <span className='text-muted-foreground/60 text-xs'>
                / {props.totalCount.toLocaleString()}
              </span>
            )}
          </div>
        </div>

        <div className='flex flex-wrap items-center gap-2'>
          <div className='flex w-full items-center gap-2 sm:w-auto'>
            <SegmentedControl
              options={[
                { value: 'standard', label: t('Standard') },
                { value: 'recharge', label: t('Recharge') },
              ]}
              value={props.showRechargePrice ? 'recharge' : 'standard'}
              onChange={handleRechargePriceChange}
              ariaLabel={t('Price display mode')}
            />
            <SegmentedControl
              options={[
                { value: 'M', label: '/1M' },
                { value: 'K', label: '/1K' },
              ]}
              value={props.tokenUnit}
              onChange={handleTokenUnitChange}
              ariaLabel={t('Token unit')}
            />
          </div>

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button
                type='button'
                variant='outline'
                size='sm'
                className='h-8 px-3 text-xs'
              >
                <ArrowUpDown data-icon='inline-start' />
                <span>
                  {sortLabels[props.sortBy as SortOption] || t('Sort')}
                </span>
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align='end' className='w-44'>
              <DropdownMenuGroup>
                {Object.entries(sortLabels).map(([value, label]) => (
                  <DropdownMenuItem
                    key={value}
                    onClick={() => props.onSortChange(value)}
                    className='gap-2'
                  >
                    <Check
                      className={cn(
                        'size-4 shrink-0',
                        props.sortBy === value ? 'opacity-100' : 'opacity-0'
                      )}
                    />
                    {label}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      <Sheet open={mobileFiltersOpen} onOpenChange={setMobileFiltersOpen}>
        <SheetContent
          side='right'
          className='flex h-dvh w-full flex-col overflow-hidden p-0 sm:max-w-md'
        >
          <SheetHeader className='border-b px-4 py-3 sm:px-6 sm:py-4'>
            <SheetTitle>{t('Filter')}</SheetTitle>
            <SheetDescription>
              {t('Filter models by provider, type, endpoint, and tags.')}
            </SheetDescription>
          </SheetHeader>
          <div className='flex-1 overflow-y-auto p-3 sm:p-4'>
            <PricingSidebar
              quotaTypeFilter={props.quotaTypeFilter}
              endpointTypeFilter={props.endpointTypeFilter}
              vendorFilter={props.vendorFilter}
              groupFilter={props.groupFilter}
              tagFilter={props.tagFilter}
              onQuotaTypeChange={props.onQuotaTypeChange}
              onEndpointTypeChange={props.onEndpointTypeChange}
              onVendorChange={props.onVendorChange}
              onTagChange={props.onTagChange}
              vendors={props.vendors}
              tags={props.tags}
              models={props.models}
              groupEndpointOverrides={props.groupEndpointOverrides}
              hasActiveFilters={props.hasActiveFilters}
              onClearFilters={props.onClearFilters}
              showHeader={false}
              className='border-0 bg-transparent p-0 shadow-none'
            />
          </div>
        </SheetContent>
      </Sheet>
    </div>
  )
}
