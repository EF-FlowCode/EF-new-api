import { useCallback, useEffect, useMemo } from 'react'
import {
  type ColumnDef,
  getCoreRowModel,
  type Table,
  useReactTable,
} from '@tanstack/react-table'
import { type NavigateFn, useTableUrlState } from '@/hooks/use-table-url-state'
import {
  getServerPageResetPatch,
  getServerPaginationMeta,
  type ServerPaginationConfig,
  type ServerPaginationSearch,
} from './server-pagination-state'

const EMPTY_ROWS: unknown[] = []
const EMPTY_COLUMNS: ColumnDef<unknown>[] = []

type UseServerPaginationParams = ServerPaginationConfig & {
  search: ServerPaginationSearch
  navigate: NavigateFn
  total: number
}

export type ServerPaginationController = {
  table: Table<unknown>
  page: number
  pageSize: number
  pageCount: number
  resetPage: (searchPatch?: ServerPaginationSearch) => void
}

export function useServerPagination({
  search,
  navigate,
  total,
  pageKey,
  pageSizeKey,
  defaultPage = 1,
  defaultPageSize = 20,
}: UseServerPaginationParams): ServerPaginationController {
  const config = useMemo(
    () => ({ pageKey, pageSizeKey, defaultPage, defaultPageSize }),
    [pageKey, pageSizeKey, defaultPage, defaultPageSize]
  )
  const { pagination, onPaginationChange, ensurePageInRange } =
    useTableUrlState({
      search,
      navigate,
      pagination: config,
      globalFilter: { enabled: false },
    })
  const { pageCount } = getServerPaginationMeta(total, pagination)

  const table = useReactTable({
    data: EMPTY_ROWS,
    columns: EMPTY_COLUMNS,
    state: { pagination },
    onPaginationChange,
    getCoreRowModel: getCoreRowModel(),
    manualPagination: true,
    pageCount,
    rowCount: Math.max(0, total),
  })

  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [ensurePageInRange, pageCount])

  const resetPage = useCallback(
    (searchPatch: ServerPaginationSearch = {}) => {
      navigate({
        search: (previous) => ({
          ...previous,
          ...getServerPageResetPatch(config),
          ...searchPatch,
        }),
      })
    },
    [config, navigate]
  )

  return {
    table,
    page: pagination.pageIndex + 1,
    pageSize: pagination.pageSize,
    pageCount,
    resetPage,
  }
}
