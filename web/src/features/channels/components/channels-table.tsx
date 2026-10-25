import { useState, useMemo, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  flexRender,
  getCoreRowModel,
  useReactTable,
  getExpandedRowModel,
  type SortingState,
  type VisibilityState,
  type ExpandedState,
  type Row,
} from '@tanstack/react-table'
import { useMediaQuery } from '@/hooks'
import { useTranslation } from 'react-i18next'
import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTableToolbar,
  TableSkeleton,
  TableEmpty,
  MobileCardList,
} from '@/components/data-table'
import { DataTablePagination } from '@/components/data-table/pagination'
import { PageFooterPortal } from '@/components/layout'
import { getChannels, searchChannels, getGroups } from '../api'
import {
  DEFAULT_PAGE_SIZE,
  CHANNEL_STATUS,
  CHANNEL_STATUS_OPTIONS,
} from '../constants'
import {
  channelsQueryKeys,
  aggregateChannelsByTag,
  getChannelTableRowId,
  isTagAggregateRow,
  getChannelTypeIcon,
  getChannelTypeLabel,
  loadChannelColumnVisibility,
  saveChannelColumnVisibility,
} from '../lib'
import type { Channel } from '../types'
import { useChannelsColumns } from './channels-columns'
import { useChannels } from './channels-provider'
import { DataTableBulkActions } from './data-table-bulk-actions'

const route = getRouteApi('/_authenticated/channels/')

function isDisabledChannelRow(channel: Channel) {
  return (
    !isTagAggregateRow(channel) && channel.status !== CHANNEL_STATUS.ENABLED
  )
}

export function ChannelsTable() {
  const { t } = useTranslation()
  const { enableTagMode, idSort, setCurrentRow, setOpen } = useChannels()
  const isMobile = useMediaQuery('(max-width: 640px)')

  // Table state
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(
    loadChannelColumnVisibility
  )
  const [rowSelection, setRowSelection] = useState({})
  const [expanded, setExpanded] = useState<ExpandedState>({})

  // URL state management
  const {
    globalFilter,
    onGlobalFilterChange,
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: {
      defaultPage: 1,
      defaultPageSize: isMobile ? 10 : DEFAULT_PAGE_SIZE,
    },
    globalFilter: { enabled: true, key: 'filter' },
    columnFilters: [
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'type', searchKey: 'type', type: 'array' },
      { columnId: 'group', searchKey: 'group', type: 'array' },
      { columnId: 'address', searchKey: 'address', type: 'array' },
      { columnId: 'model', searchKey: 'model', type: 'string' },
    ],
  })

  // Extract filters from column filters
  const statusFilter =
    (columnFilters.find((f) => f.id === 'status')?.value as string[]) || []
  // typeFilter 用作下游 useMemo 的依赖，必须返回稳定引用——空数组用 useMemo 兜住
  const typeFilter = useMemo(
    () => (columnFilters.find((f) => f.id === 'type')?.value as string[]) || [],
    [columnFilters]
  )
  const groupFilter =
    (columnFilters.find((f) => f.id === 'group')?.value as string[]) || []
  const addressFilterValue = (
    (columnFilters.find((f) => f.id === 'address')?.value as string[]) || []
  ).find((value) => value !== 'all')
  const modelFilterFromUrl =
    (columnFilters.find((f) => f.id === 'model')?.value as string) || ''

  // Local draft; commit the model filter together with the main search button.
  const [modelFilterInput, setModelFilterInput] = useState(modelFilterFromUrl)

  // Sync local input with URL when URL changes (e.g., from back/forward navigation)
  useEffect(() => {
    setModelFilterInput(modelFilterFromUrl)
  }, [modelFilterFromUrl])

  const applyModelFilter = () => {
    const value = modelFilterInput.trim()
    if (value === modelFilterFromUrl) return
    onColumnFiltersChange((prev) => {
      const filtered = prev.filter((f) => f.id !== 'model')
      return value ? [...filtered, { id: 'model', value }] : filtered
    })
  }

  const modelFilter = modelFilterFromUrl

  // Determine whether to use search or regular list API
  const shouldSearch = Boolean(globalFilter?.trim() || modelFilter.trim())

  // Fetch groups for filter
  const { data: groupsData } = useQuery({
    queryKey: ['groups'],
    queryFn: getGroups,
  })

  const groupOptions = useMemo(
    () =>
      (groupsData?.data || []).map((g) => ({
        label: g,
        value: g,
      })),
    [groupsData]
  )

  // 从 tanstack sorting state 抽出后端 sort_by/sort_order——只取第一个排序条件
  // （后端不支持多列排序）；列 id 即后端白名单字段
  // (id/name/priority/balance/response_time/test_time)
  const sortBy = sorting[0]?.id
  const sortOrder: 'asc' | 'desc' | undefined = sorting[0]
    ? sorting[0].desc
      ? 'desc'
      : 'asc'
    : undefined

  // Fetch channels data
  // eslint-disable-next-line @tanstack/query/exhaustive-deps
  const { data, isLoading, isFetching } = useQuery({
    queryKey: channelsQueryKeys.list({
      keyword: globalFilter,
      model: modelFilter,
      group:
        groupFilter.length > 0 && !groupFilter.includes('all')
          ? groupFilter[0]
          : undefined,
      status:
        statusFilter.length > 0 && !statusFilter.includes('all')
          ? statusFilter[0]
          : undefined,
      type:
        typeFilter.length > 0 && !typeFilter.includes('all')
          ? Number(typeFilter[0])
          : undefined,
      address: addressFilterValue,
      tag_mode: enableTagMode,
      id_sort: idSort,
      sort_by: sortBy,
      sort_order: sortOrder,
      p: pagination.pageIndex + 1,
      page_size: pagination.pageSize,
    }),
    queryFn: async () => {
      if (shouldSearch) {
        return searchChannels({
          keyword: globalFilter,
          model: modelFilter,
          group:
            groupFilter.length > 0 && !groupFilter.includes('all')
              ? groupFilter[0]
              : undefined,
          status:
            statusFilter.length > 0 && !statusFilter.includes('all')
              ? statusFilter[0]
              : undefined,
          type:
            typeFilter.length > 0 && !typeFilter.includes('all')
              ? Number(typeFilter[0])
              : undefined,
          address: addressFilterValue,
          tag_mode: enableTagMode,
          id_sort: idSort,
          sort_by: sortBy,
          sort_order: sortOrder,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
      } else {
        return getChannels({
          group:
            groupFilter.length > 0 && !groupFilter.includes('all')
              ? groupFilter[0]
              : undefined,
          status:
            statusFilter.length > 0 && !statusFilter.includes('all')
              ? statusFilter[0]
              : undefined,
          type:
            typeFilter.length > 0 && !typeFilter.includes('all')
              ? Number(typeFilter[0])
              : undefined,
          address: addressFilterValue,
          tag_mode: enableTagMode,
          id_sort: idSort,
          sort_by: sortBy,
          sort_order: sortOrder,
          p: pagination.pageIndex + 1,
          page_size: pagination.pageSize,
        })
      }
    },
    placeholderData: (previousData) => previousData,
  })

  // Apply tag aggregation if tag mode is enabled
  const channels = useMemo(() => {
    const rawChannels = data?.data?.items || []

    if (enableTagMode && rawChannels.length > 0) {
      return aggregateChannelsByTag(rawChannels)
    }

    return rawChannels
  }, [data, enableTagMode])

  const totalCount = data?.data?.total || 0
  const typeCounts = data?.data?.type_counts
  const addressCounts = data?.data?.address_counts
  const pageStats = useMemo(() => {
    const items = data?.data?.items || []
    return {
      enabled: items.filter(
        (channel) => channel.status === CHANNEL_STATUS.ENABLED
      ).length,
      disabled: items.filter(
        (channel) => channel.status !== CHANNEL_STATUS.ENABLED
      ).length,
      untested: items.filter((channel) => Number(channel.test_time) <= 0)
        .length,
    }
  }, [data])

  // Columns configuration
  const columns = useChannelsColumns()

  useEffect(() => {
    saveChannelColumnVisibility(columnVisibility)
  }, [columnVisibility])

  // React Table instance
  const table = useReactTable({
    data: channels,
    columns,
    pageCount: Math.ceil(totalCount / pagination.pageSize),
    state: {
      sorting,
      columnFilters,
      columnVisibility,
      rowSelection,
      pagination,
      expanded,
      globalFilter,
    },
    enableRowSelection: (row: Row<Channel>) => !isTagAggregateRow(row.original),
    onRowSelectionChange: setRowSelection,
    onSortingChange: setSorting,
    onColumnFiltersChange,
    onColumnVisibilityChange: setColumnVisibility,
    onPaginationChange,
    onExpandedChange: setExpanded,
    onGlobalFilterChange,
    getCoreRowModel: getCoreRowModel(),
    getExpandedRowModel: getExpandedRowModel(),
    getRowId: getChannelTableRowId,
    getSubRows: (row: Channel & { children?: Channel[] }) => row.children,
    manualPagination: true,
    manualSorting: true,
    manualFiltering: true,
  })

  // Ensure page is in range when total count changes
  const pageCount = table.getPageCount()
  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [pageCount, ensurePageInRange])

  // Prepare filter options from existing channel types only.
  const typeFilterOptions = useMemo(() => {
    const counts = typeCounts || {}
    const typeIds = Object.entries(counts)
      .map(([type, count]) => ({
        type: Number(type),
        count: Number(count) || 0,
      }))
      .filter((item) => item.type > 0 && item.count > 0)
      .sort((a, b) => {
        const labelA = t(getChannelTypeLabel(a.type))
        const labelB = t(getChannelTypeLabel(b.type))
        return labelA.localeCompare(labelB)
      })

    const selectedType = typeFilter.find((value) => value !== 'all')
    if (selectedType) {
      const selectedTypeId = Number(selectedType)
      const alreadyIncluded = typeIds.some(
        (item) => item.type === selectedTypeId
      )
      if (selectedTypeId > 0 && !alreadyIncluded) {
        typeIds.push({
          type: selectedTypeId,
          count: Number(counts[selectedType]) || 0,
        })
      }
    }

    const totalTypes = Object.values(counts).reduce(
      (sum, count) => sum + (Number(count) || 0),
      0
    )

    return [
      {
        label: 'All Types',
        value: 'all',
        count: totalTypes,
      },
      ...typeIds.map((item) => {
        const iconName = getChannelTypeIcon(item.type)
        return {
          label: getChannelTypeLabel(item.type),
          value: String(item.type),
          count: item.count,
          iconNode: getLobeIcon(`${iconName}.Color`, 16),
        }
      }),
    ]
  }, [t, typeCounts, typeFilter])

  const groupFilterOptions = [
    { label: t('All Groups'), value: 'all' },
    ...groupOptions,
  ]

  const addressFilterOptions = [
    {
      label: t('All Addresses'),
      value: 'all',
      count: (addressCounts?.ip ?? 0) + (addressCounts?.domain ?? 0),
    },
    { label: t('Domain'), value: 'domain', count: addressCounts?.domain ?? 0 },
    { label: t('IP address'), value: 'ip', count: addressCounts?.ip ?? 0 },
  ]

  return (
    <>
      <div className='space-y-3 sm:space-y-4'>
        <div className='grid grid-cols-2 gap-2 lg:grid-cols-4'>
          {[
            {
              label: t('Total Channels'),
              value: totalCount,
              dot: 'bg-sky-500',
            },
            {
              label: t('Enabled on this page'),
              value: pageStats.enabled,
              dot: 'bg-emerald-500',
            },
            {
              label: t('Disabled on this page'),
              value: pageStats.disabled,
              dot: 'bg-rose-500',
            },
            {
              label: t('Never tested on this page'),
              value: pageStats.untested,
              dot: 'bg-amber-500',
            },
          ].map((item) => (
            <div
              key={item.label}
              className='flex items-center justify-between rounded-lg border px-3 py-2.5'
            >
              <div className='flex items-center gap-2'>
                <span
                  className={cn('size-2 rounded-full', item.dot)}
                  aria-hidden='true'
                />
                <span className='text-muted-foreground text-xs font-medium'>
                  {item.label}
                </span>
              </div>
              <span className='text-lg font-semibold tabular-nums'>
                {item.value}
              </span>
            </div>
          ))}
        </div>

        <DataTableToolbar
          table={table}
          searchMode='manual'
          onSearch={applyModelFilter}
          searchPlaceholder={t('Filter by name, ID, or key...')}
          additionalSearch={
            <Input
              placeholder={t('Filter by model...')}
              value={modelFilterInput}
              onChange={(e) => setModelFilterInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  applyModelFilter()
                }
              }}
              className='h-8 w-full sm:w-[150px] lg:w-[200px]'
            />
          }
          filters={[
            {
              columnId: 'status',
              title: t('Status'),
              options: [...CHANNEL_STATUS_OPTIONS],
              singleSelect: true,
            },
            {
              columnId: 'type',
              title: t('Type'),
              options: typeFilterOptions,
              singleSelect: true,
            },
            {
              columnId: 'group',
              title: t('Group'),
              options: groupFilterOptions,
              singleSelect: true,
            },
            {
              columnId: 'address',
              title: t('Address'),
              options: addressFilterOptions,
              singleSelect: true,
            },
          ]}
        />

        {isMobile ? (
          <MobileCardList
            table={table}
            isLoading={isLoading}
            emptyTitle={t('No Channels Found')}
            emptyDescription={t(
              'No channels available. Create your first channel to get started.'
            )}
            getRowClassName={(row) =>
              isDisabledChannelRow(row.original)
                ? DISABLED_ROW_MOBILE
                : undefined
            }
          />
        ) : (
          <>
            <div
              className={cn(
                'overflow-hidden rounded-md border transition-opacity duration-150',
                isFetching && !isLoading && 'pointer-events-none opacity-50'
              )}
            >
              <Table>
                <TableHeader>
                  {table.getHeaderGroups().map((headerGroup) => (
                    <TableRow key={headerGroup.id}>
                      {headerGroup.headers.map((header) => (
                        <TableHead
                          key={header.id}
                          style={{ width: header.getSize() }}
                        >
                          {header.isPlaceholder
                            ? null
                            : flexRender(
                                header.column.columnDef.header,
                                header.getContext()
                              )}
                        </TableHead>
                      ))}
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {isLoading ? (
                    <TableSkeleton table={table} keyPrefix='channel-skeleton' />
                  ) : table.getRowModel().rows.length === 0 ? (
                    <TableEmpty
                      colSpan={columns.length}
                      title={t('No Channels Found')}
                      description={t(
                        'No channels available. Create your first channel to get started.'
                      )}
                    />
                  ) : (
                    table.getRowModel().rows.map((row) => (
                      <TableRow
                        key={row.id}
                        data-state={row.getIsSelected() && 'selected'}
                        tabIndex={
                          isTagAggregateRow(row.original) ? undefined : 0
                        }
                        aria-label={`${t('Configure')} ${row.original.name}`}
                        onClick={(event) => {
                          if (isTagAggregateRow(row.original)) return
                          const target = event.target as HTMLElement
                          if (
                            target.closest(
                              'button, a, input, [role="checkbox"], [role="menuitem"]'
                            )
                          ) {
                            return
                          }
                          setCurrentRow(row.original)
                          setOpen('update-channel')
                        }}
                        onKeyDown={(event) => {
                          if (
                            isTagAggregateRow(row.original) ||
                            event.target !== event.currentTarget ||
                            (event.key !== 'Enter' && event.key !== ' ')
                          ) {
                            return
                          }
                          event.preventDefault()
                          setCurrentRow(row.original)
                          setOpen('update-channel')
                        }}
                        className={cn(
                          !isTagAggregateRow(row.original) &&
                            'cursor-pointer focus-visible:ring-2 focus-visible:outline-none focus-visible:ring-inset',
                          isDisabledChannelRow(row.original) &&
                            DISABLED_ROW_DESKTOP
                        )}
                      >
                        {row.getVisibleCells().map((cell) => (
                          <TableCell key={cell.id}>
                            {flexRender(
                              cell.column.columnDef.cell,
                              cell.getContext()
                            )}
                          </TableCell>
                        ))}
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>

            <DataTableBulkActions table={table} />
          </>
        )}
      </div>
      <PageFooterPortal>
        <DataTablePagination table={table} />
      </PageFooterPortal>
    </>
  )
}
