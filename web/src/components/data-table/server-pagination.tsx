import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { DataTablePagination } from './pagination'

const DEFAULT_PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const

type ServerPaginationProps<TData> = {
  table: Table<TData>
  total: number
  loading?: boolean
  className?: string
  pageSizeOptions?: readonly number[]
  showJumpToPage?: boolean
}

export function ServerPagination<TData>({
  table,
  total,
  loading = false,
  className,
  pageSizeOptions = DEFAULT_PAGE_SIZE_OPTIONS,
  showJumpToPage = false,
}: ServerPaginationProps<TData>) {
  const { t } = useTranslation()
  if (total <= 0) return null

  return (
    <div
      className={cn(
        'flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between',
        className
      )}
    >
      <div className='text-muted-foreground shrink-0 text-xs'>
        {t('Total {{n}} records', { n: total })}
      </div>
      <div className='min-w-0 flex-1'>
        <DataTablePagination
          table={table}
          disabled={loading}
          pageSizeOptions={pageSizeOptions}
          showJumpToPage={showJumpToPage}
        />
      </div>
    </div>
  )
}
