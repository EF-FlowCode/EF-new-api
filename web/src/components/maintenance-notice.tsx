import { useEffect, useRef, useSyncExternalStore } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  getMaintenance,
  probeMaintenance,
  subscribeMaintenance,
} from '@/lib/maintenance'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'

export function MaintenanceNotice() {
  const { t } = useTranslation()
  const active = useSyncExternalStore(
    subscribeMaintenance,
    getMaintenance,
    () => false
  )
  const wasActive = useRef(active)
  useEffect(() => {
    if (wasActive.current && !active)
      toast.success(t('Service has been restored.'))
    wasActive.current = active
  }, [active, t])

  useEffect(() => {
    let stopped = false
    let timer: ReturnType<typeof setTimeout>
    let controller: AbortController | undefined
    const check = async () => {
      controller = new AbortController()
      const timeout = setTimeout(() => controller?.abort(), 5000)
      try {
        await probeMaintenance(controller.signal)
      } catch {
        // An outage or an older gateway is not proof of maintenance/recovery.
      } finally {
        clearTimeout(timeout)
        if (!stopped)
          timer = setTimeout(check, getMaintenance() ? 15000 : 30000)
      }
    }
    void check()
    return () => {
      stopped = true
      clearTimeout(timer)
      controller?.abort()
    }
  }, [])

  if (!active) return null
  return (
    <div
      className='fixed inset-x-4 bottom-4 z-50 mx-auto max-w-xl'
      aria-live='polite'
    >
      <Alert role='status'>
        <AlertTitle>{t('Maintenance in progress')}</AlertTitle>
        <AlertDescription>
          {t(
            'New requests are temporarily unavailable. Keep this page open; your unsaved input will remain here.'
          )}
          {t(
            'We will check for recovery automatically. Requests will not be resubmitted.'
          )}
        </AlertDescription>
      </Alert>
    </div>
  )
}
