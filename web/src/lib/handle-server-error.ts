import { AxiosError } from 'axios'
import i18next from 'i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import { isMaintenanceError } from '@/lib/maintenance'

// serverErrorMessage returns the toast for a failed mutation, or null when the
// API interceptor has already shown this HTTP error.
export function serverErrorMessage(error: unknown): string | null {
  if (isMaintenanceError(error)) return null

  if (error instanceof AxiosError && error.response) {
    const config = error.config
    if (!config?.skipErrorHandler && !config?.skipErrorNotification) {
      return null
    }
  }

  if (
    error &&
    typeof error === 'object' &&
    'status' in error &&
    Number(error.status) === 204
  ) {
    return i18next.t('Content not found.')
  }

  const message = getRequestErrorMessage(error, 'Something went wrong!')
  return i18next.exists(message) ? i18next.t(message) : message
}

export function handleServerError(error: unknown) {
  const message = serverErrorMessage(error)
  if (message) toast.error(message)
}
