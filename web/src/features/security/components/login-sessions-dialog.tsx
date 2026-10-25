import { useCallback, useEffect, useState } from 'react'
import { LogOut, MonitorSmartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  api,
  CALLER_HANDLED_REQUEST_CONFIG,
  type ApiResult,
  getRequestErrorMessage,
  unwrapApiResult,
} from '@/lib/api'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/confirm-dialog'

interface LoginSession {
  sid: string
  current: boolean
  login_method: string
  ip: string
  user_agent: string
  created_at: number
  last_active_at: number
  last_active_ip: string
}

async function listSessions(): Promise<LoginSession[]> {
  const res = await api.get<ApiResult<{ items: LoginSession[] }>>(
    '/api/user/sessions',
    CALLER_HANDLED_REQUEST_CONFIG
  )
  return unwrapApiResult(res.data, 'Failed to load sign-in sessions').items
}

function formatTime(unix?: number) {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString([], { hour12: false })
}

function describeAgent(ua: string) {
  if (!ua) return '—'
  const browser = /Edg\//.test(ua)
    ? 'Edge'
    : /Chrome\//.test(ua)
      ? 'Chrome'
      : /Firefox\//.test(ua)
        ? 'Firefox'
        : /Safari\//.test(ua)
          ? 'Safari'
          : ''
  const os = /Windows/.test(ua)
    ? 'Windows'
    : /Android/.test(ua)
      ? 'Android'
      : /iPhone|iPad/.test(ua)
        ? 'iOS'
        : /Mac OS X/.test(ua)
          ? 'macOS'
          : /Linux/.test(ua)
            ? 'Linux'
            : ''
  return [browser, os].filter(Boolean).join(' · ') || ua.slice(0, 60)
}

interface LoginSessionsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

/** Lists the account's signed-in browsers and signs out one or all others. */
export function LoginSessionsDialog({
  open,
  onOpenChange,
}: LoginSessionsDialogProps) {
  const { t } = useTranslation()
  const [items, setItems] = useState<LoginSession[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<LoginSession | 'others' | null>(null)

  const refresh = useCallback(async () => {
    try {
      setItems(await listSessions())
    } catch (error) {
      toast.error(
        getRequestErrorMessage(error, t('Failed to load sign-in sessions'))
      )
    }
  }, [t])

  useEffect(() => {
    if (open) void refresh()
  }, [open, refresh])

  const revoke = async () => {
    const target = confirm
    setConfirm(null)
    if (!target) return
    setBusy(true)
    try {
      const res =
        target === 'others'
          ? await api.post<ApiResult<null>>(
              '/api/user/sessions/revoke-others',
              undefined,
              CALLER_HANDLED_REQUEST_CONFIG
            )
          : await api.delete<ApiResult<null>>(
              `/api/user/sessions/${target.sid}`,
              CALLER_HANDLED_REQUEST_CONFIG
            )
      unwrapApiResult(res.data, 'Failed to sign out the session')
      if (target !== 'others' && target.current) {
        window.location.assign('/sign-in')
        return
      }
      toast.success(t('Signed out'))
      await refresh()
    } catch (error) {
      toast.error(
        getRequestErrorMessage(error, t('Failed to sign out the session'))
      )
    } finally {
      setBusy(false)
    }
  }

  const others = items?.filter((item) => !item.current).length ?? 0

  return (
    <>
      <Dialog open={open} onOpenChange={onOpenChange}>
        <DialogContent className='max-h-[85vh] overflow-y-auto sm:max-w-xl'>
          <DialogHeader>
            <DialogTitle className='flex items-center gap-2'>
              <MonitorSmartphone className='h-5 w-5' />
              {t('Signed-in devices')}
            </DialogTitle>
            <DialogDescription>
              {t(
                'Browsers currently signed in to your account. Sign out any you do not recognize.'
              )}
            </DialogDescription>
          </DialogHeader>

          {!items ? (
            <Skeleton className='h-32 w-full' />
          ) : (
            <ul className='divide-y rounded-lg border'>
              {items.map((item) => (
                <li
                  key={item.sid}
                  className='flex flex-col gap-2 p-3 sm:flex-row sm:items-center sm:justify-between'
                >
                  <div className='min-w-0 space-y-1 text-sm'>
                    <div className='flex items-center gap-2 font-medium'>
                      {describeAgent(item.user_agent)}
                      {item.current && (
                        <Badge variant='outline'>{t('This device')}</Badge>
                      )}
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Signed in at')}: {formatTime(item.created_at)} ·{' '}
                      {item.ip || '—'}
                      {item.login_method ? ` · ${item.login_method}` : ''}
                    </div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Last active')}:{' '}
                      {formatTime(item.last_active_at || item.created_at)}{' '}
                      {item.last_active_ip}
                    </div>
                  </div>
                  <Button
                    type='button'
                    size='sm'
                    variant='outline'
                    disabled={busy}
                    onClick={() => setConfirm(item)}
                  >
                    <LogOut className='mr-1 h-3.5 w-3.5' />
                    {t('Sign out')}
                  </Button>
                </li>
              ))}
            </ul>
          )}

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              disabled={busy || others === 0}
              onClick={() => setConfirm('others')}
            >
              {t('Sign out all other devices')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={confirm !== null}
        onOpenChange={(next) => !next && setConfirm(null)}
        title={
          confirm === 'others'
            ? t('Sign out all other devices?')
            : t('Sign out this device?')
        }
        desc={
          confirm !== 'others' && confirm?.current
            ? t('You will be signed out of this browser.')
            : t('The device will need to sign in again.')
        }
        confirmText={t('Sign out')}
        destructive
        handleConfirm={revoke}
      />
    </>
  )
}
