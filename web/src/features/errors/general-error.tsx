import { useNavigate, useRouter } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { isResourceLoadError } from '@/lib/resource-recovery'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'
import { getErrorPageStatus } from './error-status'

type GeneralErrorProps = React.HTMLAttributes<HTMLDivElement> & {
  minimal?: boolean
  error?: unknown
}

export function GeneralError({
  className,
  minimal = false,
  error,
}: GeneralErrorProps) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history } = useRouter()
  if (isResourceLoadError(error)) {
    return (
      <div
        className={cn(
          'flex min-h-svh flex-col items-center justify-center gap-4 p-6 text-center',
          className
        )}
      >
        <h1 className='text-xl font-semibold'>
          {t('Page resources could not be loaded')}
        </h1>
        <p className='text-muted-foreground max-w-md'>
          {t(
            'Check your connection and try again. Refreshing may discard unsaved changes.'
          )}
        </p>
        <Button onClick={() => window.location.reload()}>
          {t('Refresh page')}
        </Button>
      </div>
    )
  }
  const status = getErrorPageStatus(error)
  return (
    <div className={cn('h-svh w-full', className)}>
      <div className='m-auto flex h-full w-full flex-col items-center justify-center gap-2'>
        {!minimal && status !== undefined && (
          <h1 className='text-[7rem] leading-tight font-bold'>{status}</h1>
        )}
        <span className='font-medium'>
          {t('Oops! Something went wrong')} {`:')`}
        </span>
        <p className='text-muted-foreground text-center'>
          {t('We apologize for the inconvenience.')} <br />{' '}
          {t('Please try again later.')}
        </p>
        {!minimal && (
          <div className='mt-6 flex gap-4'>
            <Button variant='outline' onClick={() => history.go(-1)}>
              {t('Go Back')}
            </Button>
            <Button onClick={() => navigate({ to: '/' })}>
              {t('Back to Home')}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
