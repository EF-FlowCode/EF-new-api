import { useState } from 'react'
import {
  ChevronLeftIcon,
  ChevronRightIcon,
  DoubleArrowLeftIcon,
  DoubleArrowRightIcon,
} from '@radix-ui/react-icons'
import { type Table } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { cn, getPageNumbers } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type DataTablePaginationProps<TData> = {
  table: Table<TData>
  // eflow: 是否显示「跳至第 N 页」输入框（日志这类深翻页场景用）。默认关，不影响其它表。
  showJumpToPage?: boolean
}

export function DataTablePagination<TData>({
  table,
  showJumpToPage = false,
}: DataTablePaginationProps<TData>) {
  const { t } = useTranslation()
  const currentPage = table.getState().pagination.pageIndex + 1
  const totalPages = table.getPageCount()
  const pageNumbers = getPageNumbers(currentPage, totalPages)

  // eflow: 跳页输入框状态。回车或点「跳转」生效，越界自动夹到 [1, totalPages]。
  const [jumpValue, setJumpValue] = useState('')
  if (table.getRowCount() === 0) return null

  const applyJump = () => {
    const n = Number(jumpValue)
    if (!Number.isFinite(n) || n < 1) return
    const clamped = Math.min(Math.max(1, Math.trunc(n)), totalPages)
    table.setPageIndex(clamped - 1)
    setJumpValue('')
  }

  return (
    <div
      className={cn(
        'flex items-center justify-between overflow-clip',
        '@max-2xl/content:flex-col-reverse @max-2xl/content:gap-2 sm:@max-2xl/content:gap-4'
      )}
      style={{ overflowClipMargin: 1 }}
    >
      <div className='flex w-full items-center justify-between gap-2'>
        <div className='flex min-w-0 items-center text-xs font-medium whitespace-nowrap sm:min-w-[130px] sm:text-sm @2xl/content:hidden'>
          {t('Page {{current}} of {{total}}', {
            current: currentPage,
            total: totalPages,
          })}
        </div>
        <div className='flex items-center gap-2 @max-2xl/content:flex-row-reverse'>
          <Select
            value={`${table.getState().pagination.pageSize}`}
            onValueChange={(value) => {
              table.setPageSize(Number(value))
            }}
          >
            <SelectTrigger className='h-8 w-[64px] sm:w-[70px]'>
              <SelectValue placeholder={table.getState().pagination.pageSize} />
            </SelectTrigger>
            <SelectContent side='top'>
              {[10, 20, 30, 40, 50, 100].map((pageSize) => (
                <SelectItem key={pageSize} value={`${pageSize}`}>
                  {pageSize}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className='hidden text-sm font-medium sm:block'>
            {t('Rows per page')}
          </p>
        </div>
      </div>

      <div className='flex items-center sm:space-x-6 lg:space-x-8'>
        <div className='flex min-w-[130px] items-center text-sm font-medium whitespace-nowrap @max-3xl/content:hidden'>
          {t('Page {{current}} of {{total}}', {
            current: currentPage,
            total: totalPages,
          })}
        </div>
        <div className='flex items-center space-x-1.5 sm:space-x-2'>
          <Button
            variant='outline'
            className='size-8 p-0 @max-md/content:hidden'
            onClick={() => table.setPageIndex(0)}
            disabled={!table.getCanPreviousPage()}
          >
            <span className='sr-only'>{t('Go to first page')}</span>
            <DoubleArrowLeftIcon className='h-4 w-4' />
          </Button>
          <Button
            variant='outline'
            className='size-8 p-0'
            onClick={() => table.previousPage()}
            disabled={!table.getCanPreviousPage()}
          >
            <span className='sr-only'>{t('Go to previous page')}</span>
            <ChevronLeftIcon className='h-4 w-4' />
          </Button>

          {/* Page number buttons */}
          {pageNumbers.map((pageNumber, index) => (
            <div key={`${pageNumber}-${index}`} className='flex items-center'>
              {pageNumber === '...' ? (
                <span className='text-muted-foreground px-1 text-sm'>...</span>
              ) : (
                <Button
                  variant={currentPage === pageNumber ? 'default' : 'outline'}
                  className='h-8 min-w-8 px-2'
                  onClick={() => table.setPageIndex((pageNumber as number) - 1)}
                >
                  <span className='sr-only'>Go to page {pageNumber}</span>
                  {pageNumber}
                </Button>
              )}
            </div>
          ))}

          <Button
            variant='outline'
            className='size-8 p-0'
            onClick={() => table.nextPage()}
            disabled={!table.getCanNextPage()}
          >
            <span className='sr-only'>{t('Go to next page')}</span>
            <ChevronRightIcon className='h-4 w-4' />
          </Button>
          <Button
            variant='outline'
            className='size-8 p-0 @max-md/content:hidden'
            onClick={() => table.setPageIndex(table.getPageCount() - 1)}
            disabled={!table.getCanNextPage()}
          >
            <span className='sr-only'>{t('Go to last page')}</span>
            <DoubleArrowRightIcon className='h-4 w-4' />
          </Button>

          {/* eflow: 跳至第 N 页 —— 深翻页时不用一格格点 */}
          {showJumpToPage && totalPages > 1 && (
            <div className='ml-1 flex items-center gap-1.5 text-sm whitespace-nowrap @max-md/content:hidden'>
              <span className='text-muted-foreground'>{t('Go to')}</span>
              <input
                type='number'
                min={1}
                max={totalPages}
                value={jumpValue}
                onChange={(e) => setJumpValue(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') applyJump()
                }}
                aria-label={t('Page number')}
                className='border-input bg-background h-8 w-16 rounded-md border px-2 text-center text-sm'
              />
              <Button
                variant='outline'
                className='h-8 px-2.5'
                onClick={applyJump}
                disabled={!jumpValue}
              >
                {t('Go')}
              </Button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
