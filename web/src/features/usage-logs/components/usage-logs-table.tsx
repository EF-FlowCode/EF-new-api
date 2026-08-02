import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  type ColumnDef,
  type SortingState,
  flexRender,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { useMediaQuery } from '@/hooks'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useIsAdmin } from '@/hooks/use-admin'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  DataTablePagination,
  DataTableViewOptions,
  TableSkeleton,
  TableEmpty,
  MobileCardList,
} from '@/components/data-table'
import { PageFooterPortal } from '@/components/layout'
import { DEFAULT_LOGS_DATA, LOG_TYPE_ENUM } from '../constants'
import { useColumnsByCategory } from '../lib/columns'
import { fetchLogsByCategory } from '../lib/utils'
import type { LogCategory } from '../types'
import { CommonLogsFilterBar } from './common-logs-filter-bar'
import { CommonLogsStats } from './common-logs-stats'
import { TaskLogsFilterBar } from './task-logs-filter-bar'

const route = getRouteApi('/_authenticated/usage-logs/$section')

const logTypeRowTint: Record<number, string> = {
  [LOG_TYPE_ENUM.ERROR]: 'bg-rose-50/40 dark:bg-rose-950/20',
  [LOG_TYPE_ENUM.REFUND]: 'bg-blue-50/30 dark:bg-blue-950/15',
}

interface UsageLogsTableProps {
  logCategory: LogCategory
}

export function UsageLogsTable({ logCategory }: UsageLogsTableProps) {
  const { t } = useTranslation()
  const isAdmin = useIsAdmin()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const searchParams = route.useSearch()

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search: route.useSearch(),
    navigate: route.useNavigate(),
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 20 : 100 },
    globalFilter: { enabled: false },
    columnFilters: [
      { columnId: 'created_at', searchKey: 'type', type: 'array' as const },
      { columnId: 'model_name', searchKey: 'model', type: 'string' as const },
      { columnId: 'token_name', searchKey: 'token', type: 'string' as const },
      { columnId: 'group', searchKey: 'group', type: 'string' as const },
      ...(isAdmin
        ? [
            {
              columnId: 'channel',
              searchKey: 'channel',
              type: 'string' as const,
            },
            {
              columnId: 'username',
              searchKey: 'username',
              type: 'string' as const,
            },
          ]
        : []),
    ],
  })

  // 服务端排序——只取第一个排序条件（后端不支持多列）；列 id 即后端 logSortColumns
  // 白名单 key (id/created_at/quota/use_time/prompt_tokens/completion_tokens)
  const [sorting, setSorting] = useState<SortingState>([])
  const sortBy = sorting[0]?.id
  const sortOrder: 'asc' | 'desc' | undefined = sorting[0]
    ? sorting[0].desc
      ? 'desc'
      : 'asc'
    : undefined

  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'logs',
      logCategory,
      isAdmin,
      pagination.pageIndex + 1,
      pagination.pageSize,
      columnFilters,
      searchParams,
      sortBy,
      sortOrder,
      t,
    ],
    queryFn: async () => {
      const result = await fetchLogsByCategory({
        logCategory,
        isAdmin,
        page: pagination.pageIndex + 1,
        pageSize: pagination.pageSize,
        searchParams,
        columnFilters,
        sortBy,
        sortOrder,
      })

      if (!result?.success) {
        toast.error(result?.message || t('Failed to load logs'))
        return DEFAULT_LOGS_DATA
      }

      return result.data || DEFAULT_LOGS_DATA
    },
    placeholderData: (previousData, previousQuery) => {
      if (previousQuery?.queryKey[1] === logCategory) {
        return previousData
      }
      return undefined
    },
  })

  const logs = data?.items || []
  const columns = useColumnsByCategory(logCategory, isAdmin)
  const isLoadingData = isLoading || (isFetching && !data)

  const table = useReactTable({
    data: logs as Record<string, unknown>[],
    columns: columns as ColumnDef<Record<string, unknown>>[],
    state: {
      columnFilters,
      pagination,
      sorting,
    },
    enableRowSelection: false,
    onPaginationChange,
    onColumnFiltersChange,
    onSortingChange: setSorting,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    pageCount: Math.ceil((data?.total || 0) / pagination.pageSize),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [pageCount, ensurePageInRange])

  const isCommon = logCategory === 'common'
  const emptyDescription = isCommon
    ? t(
        'No usage logs available. Logs will appear here once API calls are made.'
      )
    : t('No task records are available for the selected time range.')

  const renderRows = () => {
    const rows = table.getRowModel().rows
    if (rows.length === 0) return null

    return rows.map((row) => {
      const logType = (row.original as Record<string, unknown>).type as
        | number
        | undefined
      const tintClass =
        isCommon && logType != null ? (logTypeRowTint[logType] ?? '') : ''

      return (
        <TableRow key={row.id} className={cn('transition-colors', tintClass)}>
          {row.getVisibleCells().map((cell) => (
            <TableCell key={cell.id} className={isCommon ? 'py-2' : 'py-3.5'}>
              {flexRender(cell.column.columnDef.cell, cell.getContext())}
            </TableCell>
          ))}
        </TableRow>
      )
    })
  }

  return (
    <>
      <div className='space-y-3 sm:space-y-4'>
        {logCategory === 'common' ? (
          <div className='bg-card/50 rounded-md border p-2 shadow-xs sm:p-3'>
            <CommonLogsFilterBar
              stats={<CommonLogsStats />}
              viewOptions={<DataTableViewOptions table={table} />}
            />
          </div>
        ) : (
          <div className='bg-card/50 rounded-md border p-2 shadow-xs sm:p-3'>
            <TaskLogsFilterBar
              logCategory={logCategory}
              viewOptions={<DataTableViewOptions table={table} />}
            />
          </div>
        )}
        {isMobile ? (
          <MobileCardList
            table={table}
            isLoading={isLoadingData}
            emptyTitle={t('No Logs Found')}
            emptyDescription={emptyDescription}
          />
        ) : (
          <div
            className={cn(
              'overflow-hidden rounded-md border transition-opacity duration-150',
              isFetching && !isLoadingData && 'pointer-events-none opacity-50'
            )}
          >
            <Table>
              <TableHeader className='bg-muted/30 sticky top-0 z-10'>
                {table.getHeaderGroups().map((headerGroup) => (
                  <TableRow key={headerGroup.id}>
                    {headerGroup.headers.map((header) => (
                      <TableHead key={header.id} colSpan={header.colSpan}>
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
                {isLoadingData ? (
                  <TableSkeleton table={table} keyPrefix='usage-log-skeleton' />
                ) : table.getRowModel().rows.length === 0 ? (
                  <TableEmpty
                    colSpan={columns.length}
                    title={t('No Logs Found')}
                    description={emptyDescription}
                  />
                ) : (
                  renderRows()
                )}
              </TableBody>
            </Table>
          </div>
        )}
      </div>
      <PageFooterPortal>
        <DataTablePagination table={table} showJumpToPage />
      </PageFooterPortal>
    </>
  )
}
