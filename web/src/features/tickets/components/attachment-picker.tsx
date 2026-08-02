import { useTranslation } from 'react-i18next'
import { File as FileIcon, Image as ImageIcon, Loader2, Paperclip, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'
import type { UseTicketAttachmentsResult } from '../hooks/use-ticket-attachments'

const humanSize = (bytes: number): string => {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

const isImage = (mime: string): boolean =>
  typeof mime === 'string' && mime.toLowerCase().startsWith('image/')

type Props = {
  state: UseTicketAttachmentsResult
  disabled?: boolean
  className?: string
}

export function AttachmentPicker({ state, disabled = false, className }: Props) {
  const { t } = useTranslation()
  const {
    config,
    attachments,
    uploading,
    openFilePicker,
    removeAttachment,
    fileInputRef,
    onFileInputChange,
  } = state

  if (!config.enabled) return null

  return (
    <div className={cn('space-y-2', className)}>
      <div className='flex flex-wrap items-center gap-2'>
        <Button
          type='button'
          variant='outline'
          size='sm'
          onClick={openFilePicker}
          disabled={disabled || uploading || attachments.length >= config.maxCount}
        >
          {uploading ? (
            <Loader2 className='mr-1.5 h-3.5 w-3.5 animate-spin' />
          ) : (
            <Paperclip className='mr-1.5 h-3.5 w-3.5' />
          )}
          {t('Attach files')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t(
            'At most {{n}} files, each up to {{mb}} MB. Paste from clipboard supported.',
            {
              n: config.maxCount,
              mb: Math.floor(config.maxSize / 1024 / 1024),
            }
          )}
        </span>
        <input
          ref={fileInputRef}
          type='file'
          multiple
          accept={config.accept}
          className='hidden'
          onChange={onFileInputChange}
        />
      </div>

      {attachments.length > 0 && (
        <ul className='flex flex-wrap gap-2'>
          {attachments.map((att) => {
            const Icon = isImage(att.mime_type) ? ImageIcon : FileIcon
            return (
              <li
                key={att.id}
                className='bg-muted/40 inline-flex max-w-full items-center gap-2 rounded-md border px-2 py-1 text-xs'
              >
                <Icon className='text-muted-foreground h-3 w-3 shrink-0' />
                <span className='max-w-[200px] truncate' title={att.file_name}>
                  {att.file_name}
                </span>
                <span className='text-muted-foreground shrink-0'>
                  {humanSize(att.size)}
                </span>
                <button
                  type='button'
                  onClick={() => removeAttachment(att.id)}
                  disabled={disabled}
                  className='text-muted-foreground hover:text-destructive ml-0.5 shrink-0 disabled:opacity-50'
                  aria-label={t('Remove attachment')}
                >
                  <X className='h-3 w-3' />
                </button>
              </li>
            )
          })}
        </ul>
      )}
    </div>
  )
}
