import { useState, useMemo } from 'react'
import { type Table } from '@tanstack/react-table'
import { ListX, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { CopyButton } from '@/components/copy-button'
import { DataTableBulkActions as BulkActionsToolbar } from '@/components/data-table'
import { batchDeleteRedemptions, deleteInvalidRedemptions } from '../api'
import { type Redemption } from '../types'
import { useRedemptions } from './redemptions-provider'

type DataTableBulkActionsProps<TData> = {
  table: Table<TData>
}

export function DataTableBulkActions<TData>({
  table,
}: DataTableBulkActionsProps<TData>) {
  const { t } = useTranslation()
  const { triggerRefresh } = useRedemptions()
  const [showDeleteInvalidConfirm, setShowDeleteInvalidConfirm] =
    useState(false)
  const [isDeleting, setIsDeleting] = useState(false)
  // eflow: 批量删除选中的兑换码（按上游 524455fac）。确认时锁定当时的选择。
  const [deleteTargets, setDeleteTargets] = useState<Redemption[] | null>(null)
  const [isDeletingSelected, setIsDeletingSelected] = useState(false)
  const selectedRows = table.getFilteredSelectedRowModel().rows

  const contentToCopy = useMemo(() => {
    const selectedCodes = selectedRows.map((row) => {
      const redemption = row.original as Redemption
      return `${redemption.name}\t${redemption.key}`
    })
    return selectedCodes.join('\n')
  }, [selectedRows])

  const handleDeleteInvalid = async () => {
    setIsDeleting(true)
    try {
      const result = await deleteInvalidRedemptions()

      if (result.success) {
        const count = result.data || 0
        toast.success(
          t('Successfully deleted {{count}} invalid redemption codes', {
            count,
          })
        )
        table.resetRowSelection()
        triggerRefresh()
        setShowDeleteInvalidConfirm(false)
      }
    } finally {
      setIsDeleting(false)
    }
  }

  const handleDeleteSelected = async () => {
    if (!deleteTargets?.length || isDeletingSelected) return
    setIsDeletingSelected(true)
    try {
      const result = await batchDeleteRedemptions(
        deleteTargets.map((code) => code.id)
      )
      if (result.success) {
        toast.success(
          t('Successfully deleted {{count}} redemption codes', {
            count: result.data ?? 0,
          })
        )
        table.resetRowSelection()
        setDeleteTargets(null)
        triggerRefresh()
      }
    } finally {
      setIsDeletingSelected(false)
    }
  }

  return (
    <>
      <BulkActionsToolbar table={table} entityName={t('redemption code')}>
        <CopyButton
          value={contentToCopy}
          variant='outline'
          size='icon'
          className='size-8'
          tooltip={t('Copy selected codes')}
          successTooltip={t('Codes copied!')}
          aria-label={t('Copy selected codes')}
        />

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant='destructive'
              size='icon'
              className='size-8'
              disabled={isDeletingSelected}
              onClick={() =>
                setDeleteTargets(
                  selectedRows.map((row) => row.original as Redemption)
                )
              }
              aria-label={t('Delete selected redemption codes')}
            >
              <Trash2 />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Delete selected redemption codes')}</p>
          </TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant='destructive'
              size='icon'
              onClick={() => setShowDeleteInvalidConfirm(true)}
              className='size-8'
              aria-label={t('Delete invalid redemption codes')}
              title={t('Delete invalid redemption codes')}
            >
              <ListX />
              <span className='sr-only'>{t('Delete invalid codes')}</span>
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>{t('Delete invalid codes (used/disabled/expired)')}</p>
          </TooltipContent>
        </Tooltip>
      </BulkActionsToolbar>

      <ConfirmDialog
        destructive
        open={showDeleteInvalidConfirm}
        onOpenChange={setShowDeleteInvalidConfirm}
        handleConfirm={handleDeleteInvalid}
        isLoading={isDeleting}
        className='max-w-md'
        title={t('Delete Invalid Redemption Codes?')}
        desc={
          <>
            {t('This will delete all')} <strong>{t('used')}</strong>,{' '}
            <strong>{t('disabled')}</strong>
            {t(', and')} <strong>{t('expired')}</strong>{' '}
            {t('redemption codes.')}
            <br />
            {t('This action cannot be undone.')}
          </>
        }
        confirmText={t('Delete Invalid')}
      />

      <ConfirmDialog
        destructive
        open={deleteTargets !== null}
        onOpenChange={(open) => {
          if (!open && !isDeletingSelected) setDeleteTargets(null)
        }}
        handleConfirm={handleDeleteSelected}
        isLoading={isDeletingSelected}
        disabled={!deleteTargets?.length}
        className='max-w-md'
        title={t('Delete {{count}} redemption codes?', {
          count: deleteTargets?.length ?? 0,
        })}
        desc={t('This action cannot be undone.')}
        confirmText={t('Delete')}
      />
    </>
  )
}
