import { Database } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from '@/components/ui/empty'
import { TableRow, TableCell } from '@/components/ui/table'

interface TableEmptyProps {
  /**
   * Number of columns to span
   */
  colSpan: number
  /**
   * Custom title for empty state
   * @default 'No Data'
   */
  title?: string
  /**
   * Custom description for empty state
   * @default 'No records found. Try adjusting your filters.'
   */
  description?: string
  /**
   * Custom icon component
   * @default Database icon
   */
  icon?: React.ReactNode
  /**
   * Additional content to display (e.g., buttons)
   */
  children?: React.ReactNode
}

/**
 * 非表格列表（手机卡片列表等）的空状态，与 TableEmpty 同一套样式
 */
export function ListEmpty({
  title,
  description,
  icon,
  children,
}: Omit<TableEmptyProps, 'colSpan'>) {
  const { t } = useTranslation()
  // 只给了标题时不再追加「调整筛选条件」，没设筛选时这句会误导
  const resolvedDescription =
    description ??
    (title ? '' : t('No records found. Try adjusting your filters.'))
  return (
    <Empty className='py-12'>
      <EmptyHeader>
        <EmptyMedia variant='icon'>
          {icon || <Database className='size-6' />}
        </EmptyMedia>
        <EmptyTitle>{title ?? t('No Data')}</EmptyTitle>
        {resolvedDescription && (
          <EmptyDescription>{resolvedDescription}</EmptyDescription>
        )}
      </EmptyHeader>
      {children}
    </Empty>
  )
}

/**
 * Generic table empty state component
 * Displays a centered empty state message when table has no data
 */
export function TableEmpty({
  colSpan,
  title,
  description,
  icon,
  children,
}: TableEmptyProps) {
  const { t } = useTranslation()
  const resolvedTitle = title ?? t('No Data')
  const resolvedDescription =
    description ??
    (title ? '' : t('No records found. Try adjusting your filters.'))
  return (
    <TableRow>
      <TableCell colSpan={colSpan} className='h-[400px] p-0'>
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant='icon'>
              {icon || <Database className='size-6' />}
            </EmptyMedia>
            <EmptyTitle>{resolvedTitle}</EmptyTitle>
            {resolvedDescription && (
              <EmptyDescription>{resolvedDescription}</EmptyDescription>
            )}
          </EmptyHeader>
          {children}
        </Empty>
      </TableCell>
    </TableRow>
  )
}
