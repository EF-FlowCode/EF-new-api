import { useEffect, useRef, useState } from 'react'
import { Loader2, Send } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription } from '@/components/ui/alert'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { startTelegramBind } from '../../api'

// ============================================================================
// Telegram Bind Dialog Component
// ============================================================================

const TELEGRAM_WIDGET_SRC = 'https://telegram.org/js/telegram-widget.js?22'

interface TelegramBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  botName: string
  onSuccess: () => void
}

export function TelegramBindDialog({
  open,
  onOpenChange,
  botName,
}: TelegramBindDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Bind Telegram Account')}</DialogTitle>
          <DialogDescription>
            {t('Click the button below to bind your Telegram account')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-4'>
          <Alert>
            <Send className='h-4 w-4' />
            <AlertDescription>
              {t(
                'You will be redirected to Telegram to complete the binding process.'
              )}
            </AlertDescription>
          </Alert>

          <div className='flex flex-col items-center justify-center gap-4 rounded-lg border p-6'>
            <p className='text-muted-foreground text-sm'>
              {t('Bot:')}{' '}
              <span className='font-mono font-semibold'>@{botName}</span>
            </p>
            {open && <TelegramLoginWidget botName={botName} />}
          </div>

          <p className='text-muted-foreground text-center text-xs'>
            {t('The binding will complete automatically after authorization')}
          </p>
        </div>
      </DialogContent>
    </Dialog>
  )
}

// Mounted only while the dialog is open so every opening starts a fresh flow.
function TelegramLoginWidget({ botName }: { botName: string }) {
  const { t } = useTranslation()
  const widgetRef = useRef<HTMLDivElement>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  useEffect(() => {
    let cancelled = false
    const container = widgetRef.current
    // The callback is a plain browser redirect, so the backend ties it to
    // this flow with a one-time token issued for the logged-in user.
    startTelegramBind()
      .then((response) => {
        if (cancelled || !container) return
        if (!response.success || !response.data) {
          setError(response.message || t('Failed to start Telegram binding'))
          return
        }
        const authUrl = new URL(
          '/api/oauth/telegram/bind',
          window.location.origin
        )
        authUrl.searchParams.set('flow_token', response.data)
        const script = document.createElement('script')
        script.async = true
        script.src = TELEGRAM_WIDGET_SRC
        script.setAttribute('data-telegram-login', botName)
        script.setAttribute('data-size', 'large')
        script.setAttribute('data-request-access', 'write')
        script.setAttribute('data-auth-url', authUrl.toString())
        container.replaceChildren(script)
      })
      .catch(() => {
        if (!cancelled) setError(t('Failed to start Telegram binding'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })

    return () => {
      cancelled = true
      container?.replaceChildren()
    }
  }, [botName, t])

  return (
    <>
      {loading && (
        <Loader2 className='text-muted-foreground h-5 w-5 animate-spin' />
      )}
      {error && <p className='text-destructive text-sm'>{error}</p>}
      <div ref={widgetRef} className='flex min-h-10 justify-center' />
    </>
  )
}
