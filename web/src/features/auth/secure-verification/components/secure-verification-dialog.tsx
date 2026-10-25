import { useMemo } from 'react'
import { ShieldCheck, KeyRound, Loader2, LogIn } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { usableMethods } from '../hooks/use-secure-verification'
import type {
  SecureVerificationState,
  VerificationMethod,
  VerificationMethods,
} from '../types'

const METHOD_LABELS: Record<VerificationMethod, string> = {
  '2fa': 'Authenticator code',
  passkey: 'Passkey',
  password: 'Password',
  session: 'Recent sign-in',
}

interface SecureVerificationDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  methods: VerificationMethods
  state: SecureVerificationState
  onVerify: (method: VerificationMethod, code?: string) => void | Promise<void>
  onCancel: () => void
  onCodeChange: (code: string) => void
  onMethodChange: (method: VerificationMethod) => void
}

export function SecureVerificationDialog({
  open,
  onOpenChange,
  methods,
  state,
  onVerify,
  onCancel,
  onCodeChange,
  onMethodChange,
}: SecureVerificationDialogProps) {
  const { t } = useTranslation()
  const availableTabs: VerificationMethod[] = useMemo(
    () => usableMethods(methods),
    [methods]
  )
  // Allowed methods that cannot be used right now, with the server's reason.
  const unavailable = useMemo(
    () =>
      methods.options.filter(
        (option) =>
          !option.available ||
          (option.method === 'passkey' && !methods.passkeySupported)
      ),
    [methods]
  )

  const activeMethod =
    state.method ?? (availableTabs.length > 0 ? availableTabs[0] : null)

  const title =
    state.title ??
    (availableTabs.length
      ? 'Additional verification required'
      : 'Verification unavailable')

  const description =
    state.description ??
    (availableTabs.length
      ? 'Confirm your identity before accessing this sensitive action.'
      : 'Enable Two-factor Authentication or Passkey in your profile settings to continue.')

  const handleVerify = () => {
    if (!activeMethod) return
    const payload =
      activeMethod === '2fa' || activeMethod === 'password'
        ? state.code
        : undefined
    onVerify(activeMethod, payload)
  }

  const verifyDisabled =
    state.loading ||
    (activeMethod === '2fa' && (!state.code.trim() || state.code.length < 6)) ||
    (activeMethod === 'password' && !state.code)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        className='top-[8vh] max-w-[calc(100%-1.5rem)] translate-y-0 gap-0 overflow-hidden border-none p-0 shadow-xl sm:top-1/2 sm:max-w-md sm:translate-y-[-50%] sm:rounded-xl'
        showCloseButton={!state.loading}
      >
        <div className='bg-background flex max-h-[calc(100dvh-2rem)] flex-col'>
          <DialogHeader className='border-b px-6 py-5 text-left'>
            <DialogTitle className='flex items-center gap-2 text-lg font-semibold'>
              <ShieldCheck className='text-primary h-5 w-5' />
              {title}
            </DialogTitle>
            <DialogDescription className='text-left'>
              {description}
            </DialogDescription>
          </DialogHeader>

          <div className='flex-1 overflow-y-auto px-6 py-5'>
            {availableTabs.length === 0 ? (
              <div className='grid place-items-center gap-4 text-center'>
                <div className='bg-muted flex h-16 w-16 items-center justify-center rounded-full'>
                  <ShieldCheck className='text-muted-foreground h-8 w-8' />
                </div>
                <p className='text-muted-foreground text-sm'>
                  {unavailable.length > 0 && unavailable[0].reason
                    ? t(unavailable[0].reason)
                    : t(
                        'Enable Two-factor Authentication or Passkey in your profile to unlock sensitive operations.'
                      )}
                </p>
              </div>
            ) : (
              <Tabs
                value={activeMethod ?? availableTabs[0]}
                onValueChange={(value) =>
                  onMethodChange(value as VerificationMethod)
                }
                className='gap-4'
              >
                <TabsList>
                  {availableTabs.map((method) => (
                    <TabsTrigger key={method} value={method}>
                      {t(METHOD_LABELS[method])}
                    </TabsTrigger>
                  ))}
                </TabsList>

                <TabsContent value='session' className='space-y-3'>
                  <div className='bg-muted/50 flex items-center gap-3 rounded-lg p-4'>
                    <LogIn className='text-primary h-6 w-6 shrink-0' />
                    <p className='text-muted-foreground text-left text-sm'>
                      {t(
                        'Your account has no password, two-factor authentication or Passkey, so the sign-in you just completed confirms this action.'
                      )}
                    </p>
                  </div>
                </TabsContent>

                <TabsContent value='password' className='space-y-3'>
                  <p className='text-muted-foreground text-sm'>
                    {t('Enter your current password to continue.')}
                  </p>
                  <Input
                    type='password'
                    autoComplete='current-password'
                    value={state.code}
                    onChange={(event) => onCodeChange(event.target.value)}
                    placeholder={t('Current password')}
                    disabled={state.loading}
                    autoFocus={activeMethod === 'password'}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' && !verifyDisabled) {
                        event.preventDefault()
                        handleVerify()
                      }
                    }}
                  />
                </TabsContent>

                <TabsContent value='2fa' className='space-y-3'>
                  <p className='text-muted-foreground text-sm'>
                    {t(
                      'Enter the 6-digit Time-based One-Time Password or 8-character backup code from your authenticator app.'
                    )}
                  </p>
                  <Input
                    inputMode='numeric'
                    maxLength={8}
                    value={state.code}
                    onChange={(event) => onCodeChange(event.target.value)}
                    placeholder={t('Enter verification code')}
                    disabled={state.loading}
                    autoFocus={activeMethod === '2fa'}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' && !verifyDisabled) {
                        event.preventDefault()
                        handleVerify()
                      }
                    }}
                  />
                </TabsContent>

                <TabsContent value='passkey' className='space-y-4'>
                  <div className='bg-muted/50 flex items-center justify-center rounded-lg p-4'>
                    <div className='text-muted-foreground flex items-center gap-3'>
                      <KeyRound className='text-primary h-6 w-6' />
                      <div className='text-left text-sm'>
                        <p className='text-foreground font-medium'>
                          {t('Use your Passkey')}
                        </p>
                        <p>
                          {t(
                            'We will prompt your device to confirm using biometrics or your hardware key.'
                          )}
                        </p>
                      </div>
                    </div>
                  </div>
                </TabsContent>
              </Tabs>
            )}
            {availableTabs.length > 0 && unavailable.length > 0 && (
              <ul className='text-muted-foreground mt-4 space-y-1 text-xs'>
                {unavailable.map((option) => (
                  <li key={option.method}>
                    {t(METHOD_LABELS[option.method])}:{' '}
                    {option.method === 'passkey' && !methods.passkeySupported
                      ? t('This device does not support Passkey verification.')
                      : t(option.reason ?? 'Unavailable')}
                  </li>
                ))}
              </ul>
            )}
          </div>

          <DialogFooter className='bg-muted/30 border-t px-6 py-4 sm:flex-row sm:justify-end'>
            <Button
              type='button'
              variant='outline'
              disabled={state.loading}
              onClick={onCancel}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              onClick={handleVerify}
              disabled={availableTabs.length === 0 || verifyDisabled}
            >
              {state.loading && <Loader2 className='h-4 w-4 animate-spin' />}
              {t('Verify')}
            </Button>
          </DialogFooter>
        </div>
      </DialogContent>
    </Dialog>
  )
}
