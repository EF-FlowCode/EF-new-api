import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { MapPin } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getWebGroupRegionAccess } from '@/lib/api'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'

export function RegionAccessDialog() {
  const { t } = useTranslation()
  const [checked, setChecked] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const { data } = useQuery({
    queryKey: ['web-region-access'],
    queryFn: getWebGroupRegionAccess,
    retry: false,
    staleTime: 0,
  })

  const access = data?.data
  const popup = access?.popup
  const open = Boolean(access?.restricted && popup && !confirmed)
  if (!popup) return null

  const displayName = popup.display_name || access?.region || t('your region')
  const title = popup.title || t('Regional access notice')
  const content =
    popup.content || t('Some groups are unavailable in your region.')
  const notice = popup.notice || t('I have read and understood')
  const button = popup.button || t('I understand')

  return (
    <Dialog open={open} onOpenChange={() => undefined}>
      <DialogContent
        className='top-[8vh] max-w-[calc(100%-1.5rem)] translate-y-0 gap-0 overflow-hidden p-0 sm:top-1/2 sm:max-w-lg sm:translate-y-[-50%] sm:rounded-xl'
        showCloseButton={false}
      >
        <div className='bg-background flex max-h-[calc(100dvh-2rem)] flex-col'>
          <DialogHeader className='border-b px-6 py-5 text-left'>
            <DialogTitle className='flex items-center gap-2 text-lg font-semibold'>
              <MapPin className='text-primary h-5 w-5' />
              {title}
            </DialogTitle>
            <DialogDescription className='text-left'>
              {t('Detected region: {{region}}', { region: displayName })}
            </DialogDescription>
          </DialogHeader>

          <div className='flex-1 overflow-y-auto px-6 py-5'>
            <p className='text-foreground text-sm leading-7 whitespace-pre-wrap'>
              {content}
            </p>
          </div>

          <DialogFooter className='bg-muted/30 flex-col items-stretch gap-4 border-t px-6 py-4 sm:flex-col sm:items-stretch'>
            <label className='flex cursor-pointer items-start gap-3 text-sm'>
              <Checkbox
                checked={checked}
                onCheckedChange={(value) => setChecked(value === true)}
              />
              <span className='leading-6'>{notice}</span>
            </label>
            <Button
              type='button'
              className='w-full sm:w-auto sm:self-end'
              disabled={!checked}
              onClick={() => setConfirmed(true)}
            >
              {button}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}
