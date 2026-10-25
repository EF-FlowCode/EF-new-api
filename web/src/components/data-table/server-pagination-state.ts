import type { PaginationState } from '@tanstack/react-table'

export type ServerPaginationSearch = Record<string, unknown>

export type ServerPaginationConfig = {
  pageKey?: string
  pageSizeKey?: string
  defaultPage?: number
  defaultPageSize?: number
}

type ResolvedServerPaginationConfig = {
  pageKey: string
  pageSizeKey: string
  defaultPage: number
  defaultPageSize: number
}

export type ServerPaginationMeta = {
  pageCount: number
  canPreviousPage: boolean
  canNextPage: boolean
}

export function resolveServerPaginationConfig(
  config: ServerPaginationConfig = {}
): ResolvedServerPaginationConfig {
  return {
    pageKey: config.pageKey ?? 'page',
    pageSizeKey: config.pageSizeKey ?? 'pageSize',
    defaultPage: Math.max(1, Math.trunc(config.defaultPage ?? 1)),
    defaultPageSize: Math.max(1, Math.trunc(config.defaultPageSize ?? 20)),
  }
}

function readPositiveInteger(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) && value >= 1
    ? Math.trunc(value)
    : fallback
}

export function readServerPagination(
  search: ServerPaginationSearch,
  config: ServerPaginationConfig = {}
): PaginationState {
  const resolved = resolveServerPaginationConfig(config)
  const page = readPositiveInteger(
    search[resolved.pageKey],
    resolved.defaultPage
  )
  const pageSize = readPositiveInteger(
    search[resolved.pageSizeKey],
    resolved.defaultPageSize
  )

  return { pageIndex: page - 1, pageSize }
}

export function getServerPaginationSearchPatch(
  pagination: PaginationState,
  config: ServerPaginationConfig = {}
): ServerPaginationSearch {
  const resolved = resolveServerPaginationConfig(config)
  const page = Math.max(1, Math.trunc(pagination.pageIndex) + 1)
  const pageSize = readPositiveInteger(
    pagination.pageSize,
    resolved.defaultPageSize
  )

  return {
    [resolved.pageKey]: page <= resolved.defaultPage ? undefined : page,
    [resolved.pageSizeKey]:
      pageSize === resolved.defaultPageSize ? undefined : pageSize,
  }
}

export function getServerPageResetPatch(
  config: ServerPaginationConfig = {}
): ServerPaginationSearch {
  const { pageKey } = resolveServerPaginationConfig(config)
  return { [pageKey]: undefined }
}

export function getOutOfRangePagePatch(
  search: ServerPaginationSearch,
  pageCount: number,
  config: ServerPaginationConfig = {},
  resetTo: 'first' | 'last' = 'first'
): ServerPaginationSearch | null {
  const resolved = resolveServerPaginationConfig(config)
  const { pageIndex } = readServerPagination(search, resolved)
  if (pageCount <= 0 || pageIndex < pageCount) return null

  return {
    [resolved.pageKey]: resetTo === 'last' ? pageCount : undefined,
  }
}

export function getServerPaginationMeta(
  total: number,
  pagination: PaginationState
): ServerPaginationMeta {
  const normalizedTotal =
    Number.isFinite(total) && total > 0 ? Math.trunc(total) : 0
  const pageSize = readPositiveInteger(pagination.pageSize, 1)
  const pageCount = Math.ceil(normalizedTotal / pageSize)

  return {
    pageCount,
    canPreviousPage: pageCount > 0 && pagination.pageIndex > 0,
    canNextPage:
      pageCount > 0 &&
      pagination.pageIndex >= 0 &&
      pagination.pageIndex + 1 < pageCount,
  }
}
