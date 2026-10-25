import { useCallback, useState } from 'react'
import { AlertTriangle, KeyRound, Loader2, ShieldAlert } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatRelativeTimestamp, toIntlLocale } from '@/lib/relative-time'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusBadge } from '@/components/status-badge'
import { usePasskeyManagement } from '@/features/auth/passkey'
import {
  SecureVerificationDialog,
  useSecureVerification,
  type VerificationMethod,
} from '@/features/auth/secure-verification'

interface PasskeyCardProps {
  loading: boolean
}

export function PasskeyCard({ loading: pageLoading }: PasskeyCardProps) {
  const { t, i18n } = useTranslation()
  const [confirmOpen, setConfirmOpen] = useState(false)

  const {
    open: verificationOpen,
    setOpen: setVerificationOpen,
    methods: verificationMethods,
    state: verificationState,
    startVerification,
    executeVerification,
    cancel: cancelVerification,
    setCode,
    switchMethod,
  } = useSecureVerification()

  const {
    status,
    loading,
    registering,
    removing,
    supported,
    enabled,
    lastUsed,
    register,
    remove,
  } = usePasskeyManagement()

  // The server decides which methods each operation accepts: registration
  // also accepts the password (or a recent sign-in) when no factor is enrolled,
  // removal only an enrolled factor.
  const handleRegister = useCallback(async () => {
    if (!supported) {
      toast.info(t('This device does not support Passkey'))
      return
    }
    await startVerification(register, {
      operation: { scope: 'passkey.register' },
      title: t('Security verification'),
      description: t('Confirm your identity before registering a Passkey.'),
    })
  }, [register, startVerification, supported, t])

  const handleRemove = useCallback(async () => {
    setConfirmOpen(false)
    await startVerification(remove, {
      operation: { scope: 'passkey.delete' },
      preferredMethod: '2fa',
      title: t('Security verification'),
      description: t(
        'Confirm your identity before removing this Passkey from your account.'
      ),
    })
  }, [remove, startVerification, t])

  // Adapt the hook's `Promise<unknown>` return into the dialog's
  // `void | Promise<void>` signature without losing error propagation
  // semantics (errors are surfaced via toast inside the hook).
  const handleDialogVerify = useCallback(
    async (method: VerificationMethod, code?: string) => {
      try {
        await executeVerification(method, code)
      } catch {
        // Errors are already surfaced by useSecureVerification via toast.
      }
    },
    [executeVerification]
  )

  if (pageLoading || loading) {
    return (
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-3 sm:p-5'>
          <Skeleton className='h-6 w-48' />
          <Skeleton className='mt-2 h-4 w-64' />
        </CardHeader>
        <CardContent className='p-3 sm:p-5'>
          <Skeleton className='h-20 w-full' />
        </CardContent>
      </Card>
    )
  }

  const formattedLastUsed =
    lastUsed && !Number.isNaN(Date.parse(lastUsed))
      ? formatRelativeTimestamp(
          Date.parse(lastUsed),
          toIntlLocale(i18n.resolvedLanguage || i18n.language)
        )
      : t('Not used yet')

  const showUnsupportedNotice = !supported && !enabled

  return (
    <>
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-3 sm:p-5'>
          <CardTitle className='text-lg tracking-tight sm:text-xl'>
            {t('Passkey Login')}
          </CardTitle>
          <CardDescription className='text-xs sm:text-sm'>
            {t('Use Passkey to sign in without entering your password.')}
          </CardDescription>
        </CardHeader>

        <CardContent className='p-3 sm:p-5'>
          <div className='space-y-6'>
            <div className='flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between xl:flex-col 2xl:flex-row'>
              <div className='flex items-start gap-4'>
                <div className='bg-muted rounded-md p-2'>
                  <KeyRound className='h-5 w-5' />
                </div>
                <div className='space-y-1'>
                  <div className='flex flex-wrap items-center gap-2'>
                    <p className='font-medium'>{t('Passkey Authentication')}</p>
                    <StatusBadge
                      label={enabled ? t('Enabled') : t('Disabled')}
                      variant={enabled ? 'success' : 'neutral'}
                      showDot
                      copyable={false}
                    />
                    {status?.backup_eligible !== undefined && (
                      <StatusBadge
                        label={
                          status.backup_eligible
                            ? status.backup_state
                              ? t('Backed up')
                              : t('Not backed up')
                            : t('No backup')
                        }
                        variant={
                          status.backup_eligible
                            ? status.backup_state
                              ? 'success'
                              : 'warning'
                            : 'neutral'
                        }
                        showDot
                        copyable={false}
                      />
                    )}
                  </div>
                  <p className='text-muted-foreground text-sm'>
                    {t('Last used:')} {formattedLastUsed}
                  </p>
                </div>
              </div>

              {!enabled && (
                <Button
                  className='w-full sm:w-auto xl:w-full 2xl:w-auto'
                  onClick={handleRegister}
                  disabled={!supported || registering}
                >
                  {registering && (
                    <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                  )}
                  {t('Enable Passkey')}
                </Button>
              )}
            </div>

            {enabled && (
              <div className='flex flex-col gap-3 border-t pt-6 sm:flex-row xl:flex-col 2xl:flex-row'>
                <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
                  <AlertDialogTrigger asChild>
                    <Button
                      variant='destructive'
                      className='flex-1'
                      disabled={removing}
                    >
                      {removing ? (
                        <Loader2 className='mr-2 h-4 w-4 animate-spin' />
                      ) : (
                        <AlertTriangle className='mr-2 h-4 w-4' />
                      )}
                      {t('Remove Passkey')}
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>
                        {t('Remove Passkey?')}
                      </AlertDialogTitle>
                      <AlertDialogDescription>
                        {t(
                          'Removing Passkey will require you to sign in with your password next time. You can re-register anytime.'
                        )}
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel disabled={removing}>
                        {t('Cancel')}
                      </AlertDialogCancel>
                      <AlertDialogAction
                        className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
                        disabled={removing}
                        onClick={(event) => {
                          event.preventDefault()
                          handleRemove()
                        }}
                      >
                        {t('Remove')}
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
              </div>
            )}

            {showUnsupportedNotice && (
              <div className='bg-muted/60 text-muted-foreground flex items-start gap-3 rounded-md p-4 text-sm'>
                <ShieldAlert className='mt-0.5 h-4 w-4 flex-shrink-0 text-amber-500' />
                <div>
                  <p className='text-foreground font-medium'>
                    {t('Passkey not supported on this device')}
                  </p>
                  <p>
                    {t(
                      'Use a compatible browser or device with biometric authentication or a security key to register a Passkey.'
                    )}
                  </p>
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      <SecureVerificationDialog
        open={verificationOpen}
        onOpenChange={(next) => setVerificationOpen(next)}
        methods={verificationMethods}
        state={verificationState}
        onVerify={handleDialogVerify}
        onCancel={cancelVerification}
        onCodeChange={setCode}
        onMethodChange={switchMethod}
      />
    </>
  )
}
