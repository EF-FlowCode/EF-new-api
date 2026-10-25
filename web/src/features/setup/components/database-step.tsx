import { Database, Server } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { StatusBadge } from '@/components/status-badge'

export function DatabaseStep() {
  const { t } = useTranslation()
  return (
    <div className='space-y-4'>
      <div className='bg-card flex items-center justify-between rounded-lg border p-4'>
        <div className='space-y-1'>
          <p className='text-muted-foreground text-sm font-medium'>
            {t('Detected database')}
          </p>
          <p className='text-foreground text-base font-semibold'>MySQL 8</p>
          <p className='text-muted-foreground text-sm'>
            {t(
              'MySQL is a production-ready relational database. Keep your credentials secure.'
            )}
          </p>
        </div>
        <StatusBadge
          label='MySQL 8'
          variant='success'
          className='cursor-default'
          copyable={false}
          icon={Database}
        />
      </div>

      <Alert className='border-emerald-200 bg-emerald-50 dark:border-emerald-900/60 dark:bg-emerald-950/40'>
        <AlertTitle className='flex items-center gap-2'>
          <Server className='size-4 text-emerald-500' />
          {t('MySQL detected')}
        </AlertTitle>
        <AlertDescription>
          {t(
            'MySQL is production ready. Ensure automated backups and a dedicated user with the minimal required privileges are configured.'
          )}
        </AlertDescription>
      </Alert>
    </div>
  )
}
