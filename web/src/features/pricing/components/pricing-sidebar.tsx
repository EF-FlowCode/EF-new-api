import type { ReactNode } from 'react'
import { ChevronDown, RotateCcw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import {
  ENDPOINT_TYPES,
  FILTER_ALL,
  QUOTA_TYPES,
  getEndpointTypeLabels,
  getQuotaTypeLabels,
} from '../constants'
import { countModelsByEndpointType, parseTags } from '../lib/filters'
import type {
  PricingEndpointOverride,
  PricingModel,
  PricingVendor,
} from '../types'

type FilterOption = {
  value: string
  label: string
  count?: number
  suffix?: string
  icon?: ReactNode
  title?: string
}

type FilterSectionProps = {
  title: string
  value: string
  options: FilterOption[]
  onChange: (value: string) => void
  defaultOpen?: boolean
}

export interface PricingSidebarProps {
  quotaTypeFilter: string
  endpointTypeFilter: string
  vendorFilter: string
  groupFilter: string
  tagFilter: string
  onQuotaTypeChange: (value: string) => void
  onEndpointTypeChange: (value: string) => void
  onVendorChange: (value: string) => void
  onTagChange: (value: string) => void
  vendors: PricingVendor[]
  tags: string[]
  models: PricingModel[]
  groupEndpointOverrides?: Record<string, PricingEndpointOverride>
  hasActiveFilters: boolean
  onClearFilters: () => void
  className?: string
  showHeader?: boolean
}

function countBy(
  models: PricingModel[],
  predicate: (model: PricingModel) => boolean
): number {
  return models.reduce((count, model) => count + (predicate(model) ? 1 : 0), 0)
}

function FilterChip(props: {
  option: FilterOption
  active: boolean
  onClick: () => void
}) {
  return (
    <button
      type='button'
      onClick={props.onClick}
      className={cn(
        'group inline-flex max-w-full items-center gap-1.5 rounded-md border px-2 py-1 text-xs font-medium transition-all',
        props.active
          ? 'border-foreground/30 bg-foreground/5 text-foreground shadow-sm'
          : 'border-border/70 bg-background text-muted-foreground hover:border-border hover:bg-muted/50 hover:text-foreground'
      )}
      title={props.option.title || props.option.label}
    >
      {props.option.icon && (
        <span className='shrink-0'>{props.option.icon}</span>
      )}
      <span className='truncate'>{props.option.label}</span>
      {props.option.count != null && (
        <Badge variant='secondary' className='px-1.5 py-0 text-[10px]'>
          {props.option.count}
        </Badge>
      )}
      {props.option.suffix && (
        <Badge variant='outline' className='px-1.5 py-0 font-mono text-[10px]'>
          {props.option.suffix}
        </Badge>
      )}
    </button>
  )
}

function FilterSection(props: FilterSectionProps) {
  const selectedOption = props.options.find(
    (option) => option.value === props.value
  )

  return (
    <Collapsible
      defaultOpen={props.defaultOpen || props.value !== FILTER_ALL}
      className='border-border/70 border-b pb-3 last:border-b-0'
    >
      <CollapsibleTrigger className='group flex w-full items-center justify-between py-2.5 text-left'>
        <span className='flex min-w-0 items-center gap-2'>
          <span className='text-foreground text-sm font-semibold'>
            {props.title}
          </span>
          {props.value !== FILTER_ALL && selectedOption && (
            <Badge
              variant='secondary'
              className='max-w-40 truncate'
              title={selectedOption.title || selectedOption.label}
            >
              {selectedOption.label}
              {selectedOption.suffix && ` · ${selectedOption.suffix}`}
            </Badge>
          )}
        </span>
        <ChevronDown className='text-muted-foreground size-4 shrink-0 transition-transform group-data-[state=open]:rotate-180' />
      </CollapsibleTrigger>
      <CollapsibleContent>
        <div className='flex flex-wrap gap-1.5'>
          {props.options.map((option) => (
            <FilterChip
              key={option.value}
              option={option}
              active={props.value === option.value}
              onClick={() => props.onChange(option.value)}
            />
          ))}
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

export function PricingSidebar(props: PricingSidebarProps) {
  const { t } = useTranslation()
  const quotaTypeLabels = getQuotaTypeLabels(t)
  const endpointTypeLabels = getEndpointTypeLabels(t)

  const vendorOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Vendors'),
      count: props.models.length,
    },
    ...props.vendors
      .map((vendor) => ({
        value: vendor.name,
        label: vendor.name,
        count: countBy(
          props.models,
          (model) => model.vendor_name === vendor.name
        ),
        icon: vendor.icon ? getLobeIcon(vendor.icon, 14) : undefined,
      }))
      .filter((vendor) => vendor.count > 0),
  ]

  const quotaOptions: FilterOption[] = [
    {
      value: QUOTA_TYPES.ALL,
      label: quotaTypeLabels[QUOTA_TYPES.ALL],
      count: props.models.length,
    },
    {
      value: QUOTA_TYPES.TOKEN,
      label: quotaTypeLabels[QUOTA_TYPES.TOKEN],
      count: countBy(props.models, (model) => model.quota_type === 0),
    },
    {
      value: QUOTA_TYPES.REQUEST,
      label: quotaTypeLabels[QUOTA_TYPES.REQUEST],
      count: countBy(props.models, (model) => model.quota_type === 1),
    },
  ]

  const tagOptions: FilterOption[] = [
    {
      value: FILTER_ALL,
      label: t('All Tags'),
      count: props.models.length,
    },
    ...props.tags.map((tag) => ({
      value: tag,
      label: tag,
      count: countBy(props.models, (model) =>
        parseTags(model.tags)
          .map((item) => item.toLowerCase())
          .includes(tag.toLowerCase())
      ),
    })),
  ]

  const endpointOptions: FilterOption[] = [
    {
      value: ENDPOINT_TYPES.ALL,
      label: endpointTypeLabels[ENDPOINT_TYPES.ALL],
      count: props.models.length,
    },
    ...Object.entries(endpointTypeLabels)
      .filter(([value]) => value !== ENDPOINT_TYPES.ALL)
      .map(([value, label]) => ({
        value,
        label,
        count: countModelsByEndpointType(
          props.models,
          value,
          props.groupFilter,
          props.groupEndpointOverrides
        ),
      }))
      .filter(
        (endpoint) =>
          endpoint.count > 0 || endpoint.value === props.endpointTypeFilter
      ),
  ]

  return (
    <aside className={cn('rounded-lg border p-3', props.className)}>
      {(props.showHeader !== false || props.hasActiveFilters) && (
        <div className='mb-2.5 flex items-center justify-between gap-2'>
          {props.showHeader !== false ? (
            <div>
              <h2 className='text-foreground text-sm font-bold'>
                {t('Filter')}
              </h2>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('Filter models by provider, type, endpoint, and tags.')}
              </p>
            </div>
          ) : (
            <Badge variant='secondary'>{t('Filters active')}</Badge>
          )}
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={props.onClearFilters}
            disabled={!props.hasActiveFilters}
            className='h-7 px-2 text-xs'
          >
            <RotateCcw data-icon='inline-start' />
            {t('Reset')}
          </Button>
        </div>
      )}

      {props.showHeader !== false && props.hasActiveFilters && (
        <Badge variant='secondary' className='mb-3'>
          {t('Filters active')}
        </Badge>
      )}

      <div className='flex flex-col gap-1'>
        <FilterSection
          title={t('All Vendors')}
          value={props.vendorFilter}
          options={vendorOptions}
          onChange={props.onVendorChange}
          defaultOpen
        />
        <FilterSection
          title={t('Model Tags')}
          value={props.tagFilter}
          options={tagOptions}
          onChange={props.onTagChange}
        />
        <FilterSection
          title={t('Pricing Type')}
          value={props.quotaTypeFilter}
          options={quotaOptions}
          onChange={props.onQuotaTypeChange}
        />
        <FilterSection
          title={t('Endpoint Type')}
          value={props.endpointTypeFilter}
          options={endpointOptions}
          onChange={props.onEndpointTypeChange}
        />
      </div>
    </aside>
  )
}
