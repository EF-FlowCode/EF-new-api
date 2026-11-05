import { useState } from 'react'
import { FileIcon, FileTextIcon, ImageOffIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Dialog, DialogContent, DialogTitle } from '@/components/ui/dialog'
import type { MessageAttachment } from '../../types'

type MessageAttachmentsProps = {
  attachments: MessageAttachment[]
  className?: string
}

function getFileLabel(attachment: MessageAttachment): string {
  if (attachment.kind === 'file') return 'PDF'
  const extension = attachment.name.split('.').pop()
  return extension && extension !== attachment.name
    ? extension.toUpperCase()
    : 'TXT'
}

export function MessageAttachments({
  attachments,
  className,
}: MessageAttachmentsProps) {
  const { t } = useTranslation()
  const [preview, setPreview] = useState<MessageAttachment | null>(null)

  if (attachments.length === 0) return null

  const images = attachments.filter((attachment) => attachment.kind === 'image')
  const files = attachments.filter((attachment) => attachment.kind !== 'image')

  return (
    <div className={cn('flex max-w-full flex-col gap-2', className)}>
      {images.length > 0 && (
        <div className='flex flex-wrap justify-end gap-2'>
          {images.map((image, index) =>
            image.url ? (
              <button
                aria-label={t('Preview image')}
                className='border-border/70 bg-muted/40 hover:border-primary/40 overflow-hidden rounded-lg border transition-colors'
                key={`${image.name}-${index}`}
                onClick={() => setPreview(image)}
                type='button'
              >
                <img
                  alt={image.name}
                  className='size-28 object-cover sm:size-32'
                  src={image.url}
                />
              </button>
            ) : (
              <div
                className='border-border/70 bg-muted/40 text-muted-foreground flex size-28 flex-col items-center justify-center gap-1 rounded-lg border border-dashed px-2 text-center text-[11px] sm:size-32'
                key={`${image.name}-${index}`}
                title={t('Not saved to local history')}
              >
                <ImageOffIcon className='size-5' />
                {t('Not saved to local history')}
              </div>
            )
          )}
        </div>
      )}

      {files.length > 0 && (
        <div className='flex flex-wrap justify-end gap-2'>
          {files.map((file, index) => {
            const isMissing = file.kind === 'file' && !file.url
            const Icon = file.kind === 'text' ? FileTextIcon : FileIcon
            return (
              <div
                className={cn(
                  'border-border/70 bg-background/80 flex max-w-64 items-center gap-2 rounded-lg border px-2.5 py-1.5 text-left shadow-xs',
                  isMissing && 'border-dashed opacity-70'
                )}
                key={`${file.name}-${index}`}
                title={isMissing ? t('Not saved to local history') : file.name}
              >
                <span className='bg-primary/10 text-primary flex size-8 shrink-0 items-center justify-center rounded-md'>
                  <Icon className='size-4' />
                </span>
                <span className='min-w-0'>
                  <span className='block truncate text-sm font-medium'>
                    {file.name}
                  </span>
                  <span className='text-muted-foreground block text-[11px]'>
                    {isMissing
                      ? t('Not saved to local history')
                      : file.kind === 'text'
                        ? `${getFileLabel(file)} · ${t('Sent as text')}`
                        : getFileLabel(file)}
                  </span>
                </span>
              </div>
            )
          })}
        </div>
      )}

      <Dialog
        onOpenChange={(open) => !open && setPreview(null)}
        open={preview !== null}
      >
        <DialogContent className='max-w-[min(92vw,64rem)] p-2 sm:max-w-[min(92vw,64rem)]'>
          <DialogTitle className='sr-only'>
            {preview?.name ?? t('Preview image')}
          </DialogTitle>
          {preview?.url && (
            <img
              alt={preview.name}
              className='max-h-[80vh] w-full rounded-md object-contain'
              src={preview.url}
            />
          )}
        </DialogContent>
      </Dialog>
    </div>
  )
}
