import { AlertCircle, AlertTriangle, Settings } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { pricingSettingsUrl } from '@/features/system-settings/models/lib/pricing-settings-link'
import { MESSAGE_STATUS } from '../constants'
import { getCurrentVersion, getVersionText } from '../lib/message-utils'
import type { Message } from '../types'

interface MessageErrorProps {
  message: Message
  className?: string
}

/**
 * Display error messages using Alert component
 * Following ai-elements pattern for error handling
 */
export function MessageError({ message, className = '' }: MessageErrorProps) {
  const { t } = useTranslation()
  const user = useAuthStore((s) => s.auth.user)
  const isAdmin = user?.role != null && user.role >= 10

  if (message.status !== MESSAGE_STATUS.ERROR) {
    return null
  }

  // content 可能是 string | ContentPart[]，错误展示统一取纯文本
  const errorContent = getVersionText(
    getCurrentVersion(message).content || 'An unknown error occurred'
  )

  if (message.errorCode === 'model_price_error') {
    return (
      <Alert variant='default' className={className}>
        <AlertTriangle className='text-orange-500' />
        <AlertTitle>{t('Model Price Not Configured')}</AlertTitle>
        <AlertDescription className='space-y-2'>
          <p>{errorContent}</p>
          {isAdmin && (
            <Button
              variant='outline'
              size='sm'
              onClick={() =>
                window.open(pricingSettingsUrl(), '_blank', 'noopener')
              }
            >
              <Settings className='mr-1 h-3.5 w-3.5' />
              {t('Go to Settings')}
            </Button>
          )}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <Alert variant='destructive' className={className}>
      <AlertCircle />
      <AlertTitle>{t('Error')}</AlertTitle>
      <AlertDescription>{errorContent}</AlertDescription>
    </Alert>
  )
}
