import { useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { type Row } from '@tanstack/react-table'
import {
  MoreHorizontal,
  Boxes,
  Pencil,
  TestTube,
  Gauge,
  DollarSign,
  Download,
  Copy,
  Power,
  PowerOff,
  Key,
  Trash2,
  RefreshCw,
  Loader2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuShortcut,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { MODEL_FETCHABLE_TYPES } from '../constants'
import {
  channelsQueryKeys,
  handleDeleteChannel,
  handleTestChannel,
  handleToggleChannelStatus,
  isChannelEnabled,
  isMultiKeyChannel,
} from '../lib'
import { parseUpstreamUpdateMeta } from '../lib/upstream-update-utils'
import type { Channel } from '../types'
import { useChannels } from './channels-provider'

interface DataTableRowActionsProps {
  row: Row<Channel>
}

export function DataTableRowActions({ row }: DataTableRowActionsProps) {
  const { t } = useTranslation()
  const channel = row.original
  const { setOpen, setCurrentRow, upstream } = useChannels()
  const queryClient = useQueryClient()
  const [deleteConfirmOpen, setDeleteConfirmOpen] = useState(false)
  const [isTesting, setIsTesting] = useState(false)
  const [isTogglingStatus, setIsTogglingStatus] = useState(false)

  const isEnabled = isChannelEnabled(channel)
  const isMultiKey = isMultiKeyChannel(channel)

  const handleEdit = () => {
    setCurrentRow(channel)
    setOpen('update-channel')
  }

  const handleTest = () => {
    setCurrentRow(channel)
    setOpen('test-channel')
  }

  const handleDirectTest = async (e: React.MouseEvent<HTMLButtonElement>) => {
    e.stopPropagation()
    setIsTesting(true)
    try {
      await handleTestChannel(channel.id, undefined, () => {
        queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
      })
    } finally {
      setIsTesting(false)
    }
  }

  const handleQueryBalance = () => {
    setCurrentRow(channel)
    setOpen('balance-query')
  }

  const handleFetchModels = () => {
    setCurrentRow(channel)
    setOpen('fetch-models')
  }

  const handleManageOllamaModels = () => {
    setCurrentRow(channel)
    setOpen('ollama-models')
  }

  const handleCopy = () => {
    setCurrentRow(channel)
    setOpen('copy-channel')
  }

  const handleManageKeys = () => {
    setCurrentRow(channel)
    setOpen('multi-key-manage')
  }

  const handleToggleStatus = async (
    e?: React.MouseEvent<HTMLButtonElement>
  ) => {
    e?.stopPropagation()
    setIsTogglingStatus(true)
    try {
      await handleToggleChannelStatus(channel.id, channel.status, queryClient)
    } finally {
      setIsTogglingStatus(false)
    }
  }

  return (
    <div className='flex items-center justify-end gap-1'>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant='ghost'
            size='icon-sm'
            onClick={handleDirectTest}
            disabled={isTesting}
            aria-label={t('Quick Test')}
          >
            {isTesting ? (
              <Loader2 className='size-4 animate-spin' />
            ) : (
              <Gauge className='size-4' />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t('Quick Test')}</TooltipContent>
      </Tooltip>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant='ghost'
            size='icon-sm'
            onClick={handleToggleStatus}
            disabled={isTogglingStatus}
            aria-label={isEnabled ? t('Disable') : t('Enable')}
            className={
              isEnabled
                ? 'text-destructive hover:text-destructive'
                : 'text-emerald-600 hover:text-emerald-600 dark:text-emerald-400 dark:hover:text-emerald-400'
            }
          >
            {isTogglingStatus ? (
              <Loader2 className='size-4 animate-spin' />
            ) : isEnabled ? (
              <PowerOff className='size-4' />
            ) : (
              <Power className='size-4' />
            )}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {isEnabled ? t('Disable') : t('Enable')}
        </TooltipContent>
      </Tooltip>

      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            variant='ghost'
            className='data-[state=open]:bg-muted flex h-8 w-8 p-0'
          >
            <MoreHorizontal className='h-4 w-4' />
            <span className='sr-only'>{t('Open menu')}</span>
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align='end' className='w-52'>
          <DropdownMenuLabel className='text-muted-foreground text-xs'>
            {t('Configuration')}
          </DropdownMenuLabel>

          <DropdownMenuItem onClick={handleEdit}>
            {t('Edit')}
            <DropdownMenuShortcut>
              <Pencil size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {isMultiKey && (
            <DropdownMenuItem onClick={handleManageKeys}>
              {t('Manage Keys')}
              <DropdownMenuShortcut>
                <Key size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuSeparator />
          <DropdownMenuLabel className='text-muted-foreground text-xs'>
            {t('Checks & Sync')}
          </DropdownMenuLabel>

          <DropdownMenuItem onClick={handleTest}>
            {t('Test with Model')}
            <DropdownMenuShortcut>
              <TestTube size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Query Balance */}
          <DropdownMenuItem onClick={handleQueryBalance}>
            {t('Query Balance')}
            <DropdownMenuShortcut>
              <DollarSign size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {/* Fetch Models */}
          <DropdownMenuItem onClick={handleFetchModels}>
            {t('Fetch Upstream Models')}
            <DropdownMenuShortcut>
              <Download size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          {MODEL_FETCHABLE_TYPES.has(channel.type) && (
            <DropdownMenuItem
              onClick={() => {
                const meta = parseUpstreamUpdateMeta(channel.settings)
                if (
                  meta.pendingAddModels.length > 0 ||
                  meta.pendingRemoveModels.length > 0
                ) {
                  upstream.openModal(
                    channel,
                    meta.pendingAddModels,
                    meta.pendingRemoveModels,
                    meta.pendingAddModels.length > 0 ? 'add' : 'remove'
                  )
                } else {
                  upstream.detectChannelUpdates(channel)
                }
              }}
            >
              {t('Check Model Updates')}
              <DropdownMenuShortcut>
                <RefreshCw size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          {channel.type === 4 && (
            <DropdownMenuItem onClick={handleManageOllamaModels}>
              {t('Manage Ollama Models')}
              <DropdownMenuShortcut>
                <Boxes size={16} />
              </DropdownMenuShortcut>
            </DropdownMenuItem>
          )}

          <DropdownMenuSeparator />
          <DropdownMenuLabel className='text-muted-foreground text-xs'>
            {t('Other Actions')}
          </DropdownMenuLabel>

          <DropdownMenuItem onClick={handleCopy}>
            {t('Copy Channel')}
            <DropdownMenuShortcut>
              <Copy size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>

          <DropdownMenuSeparator />

          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              setDeleteConfirmOpen(true)
            }}
            className='text-destructive focus:text-destructive'
          >
            {t('Delete')}
            <DropdownMenuShortcut>
              <Trash2 size={16} />
            </DropdownMenuShortcut>
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <ConfirmDialog
        open={deleteConfirmOpen}
        onOpenChange={setDeleteConfirmOpen}
        title={t('Delete Channel')}
        desc={`Are you sure you want to delete "${channel.name}"? This action cannot be undone.`}
        confirmText='Delete'
        destructive
        handleConfirm={() => {
          handleDeleteChannel(channel.id, queryClient)
          setDeleteConfirmOpen(false)
        }}
      />
    </div>
  )
}
