import { useEffect, useState } from 'react'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import { isVerificationRequiredError } from '@/lib/secure-verification'
import { useCountdown } from '@/hooks/use-countdown'
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
import { Label } from '@/components/ui/label'
import {
  SecureVerificationDialog,
  useSecureVerification,
  type SecurityProof,
} from '@/features/auth/secure-verification'
import { sendEmailVerification, bindEmail, sendOldEmailCode } from '../../api'

// ============================================================================
// Email Bind Dialog Component
// ============================================================================

interface EmailBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentEmail?: string
  onSuccess: () => void
}

export function EmailBindDialog({
  open,
  onOpenChange,
  currentEmail,
  onSuccess,
}: EmailBindDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [sendingCode, setSendingCode] = useState(false)
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [oldCode, setOldCode] = useState('')
  const [sendingOldCode, setSendingOldCode] = useState(false)
  const [oldCodeSent, setOldCodeSent] = useState(false)
  // Accounts without 2FA or a Passkey also confirm from the current address.
  const [needsOldCode, setNeedsOldCode] = useState(false)
  const verification = useSecureVerification()
  const { fetchVerificationMethods } = verification

  useEffect(() => {
    if (!open || !currentEmail) {
      setNeedsOldCode(false)
      return
    }
    void fetchVerificationMethods({ scope: 'account.binding.bind' }).then(
      (methods) =>
        setNeedsOldCode(
          !methods.options.some(
            (option) => option.method === '2fa' || option.method === 'passkey'
          )
        )
    )
  }, [open, currentEmail, fetchVerificationMethods])

  const handleSendOldCode = async () => {
    try {
      setSendingOldCode(true)
      const response = await sendOldEmailCode()
      if (response.success) {
        setOldCodeSent(true)
        toast.success(t('Confirmation code sent to your current email.'))
      } else {
        toast.error(response.message || t('Failed to send verification code'))
      }
    } catch (_error) {
      toast.error(t('Failed to send verification code'))
    } finally {
      setSendingOldCode(false)
    }
  }
  const {
    secondsLeft,
    isActive,
    start: startCountdown,
    reset: resetCountdown,
  } = useCountdown({
    initialSeconds: 60,
  })

  const handleSendCode = async () => {
    if (!email || !email.includes('@')) {
      toast.error(t('Please enter a valid email address'))
      return
    }

    try {
      setSendingCode(true)
      const response = await sendEmailVerification(email)

      if (response.success) {
        toast.success(t('Verification code sent! Please check your email.'))
        startCountdown()
      } else {
        toast.error(response.message || t('Failed to send verification code'))
      }
    } catch (_error) {
      toast.error(t('Failed to send verification code'))
    } finally {
      setSendingCode(false)
    }
  }

  const handleBind = async () => {
    if (!email || !code) {
      toast.error(t('Please enter email and verification code'))
      return
    }

    const submit = async (proof?: SecurityProof) => {
      const response = await bindEmail(
        email,
        code,
        proof?.proof_token,
        needsOldCode ? oldCode : undefined
      )
      if (response.success) {
        toast.success(t('Email bound successfully!'))
        onOpenChange(false)
        onSuccess()
        // Reset form
        setEmail('')
        setCode('')
        resetCountdown()
      } else {
        toast.error(response.message || t('Failed to bind email'))
      }
    }

    try {
      setLoading(true)
      // Changing an existing email requires a recent identity check.
      await verification.withVerification(submit, {
        operation: {
          scope: 'account.binding.bind',
          context: { provider: 'email', email: email.trim().toLowerCase() },
        },
        description: t('Confirm your identity before changing your email.'),
      })
    } catch (error) {
      if (!isVerificationRequiredError(error)) {
        toast.error(getRequestErrorMessage(error, t('Failed to bind email')))
      }
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean) => {
    if (!loading) {
      onOpenChange(open)
      if (!open) {
        // Reset form when closing
        setEmail('')
        setCode('')
        setOldCode('')
        setOldCodeSent(false)
        resetCountdown()
      }
    }
  }

  return (
    <>
      <Dialog open={open} onOpenChange={handleOpenChange}>
        <DialogContent className='sm:max-w-md'>
          <DialogHeader>
            <DialogTitle>{t('Bind Email')}</DialogTitle>
            <DialogDescription>
              {currentEmail
                ? t('Current email: {{email}}. Enter a new email to change.', {
                    email: currentEmail,
                  })
                : t('Bind an email address to your account.')}
            </DialogDescription>
          </DialogHeader>

          <div className='space-y-4 py-4'>
            <div className='space-y-2'>
              <Label htmlFor='email'>{t('Email Address')}</Label>
              <Input
                id='email'
                type='email'
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder={t('Enter your email')}
                disabled={loading}
              />
            </div>

            <div className='space-y-2'>
              <Label htmlFor='code'>{t('Verification Code')}</Label>
              <div className='flex gap-2'>
                <Input
                  id='code'
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  placeholder={t('Enter code')}
                  disabled={loading}
                  maxLength={6}
                />
                <Button
                  type='button'
                  variant='outline'
                  onClick={handleSendCode}
                  disabled={sendingCode || isActive || !email}
                >
                  {isActive
                    ? `${secondsLeft}s`
                    : sendingCode
                      ? t('Sending...')
                      : t('Send')}
                </Button>
              </div>
            </div>

            {needsOldCode && (
              <div className='space-y-2'>
                <Label htmlFor='old-code'>
                  {t('Current email confirmation code')}
                </Label>
                <div className='flex gap-2'>
                  <Input
                    id='old-code'
                    value={oldCode}
                    onChange={(e) => setOldCode(e.target.value)}
                    placeholder={t('Enter code')}
                    disabled={loading}
                    maxLength={6}
                  />
                  <Button
                    type='button'
                    variant='outline'
                    onClick={handleSendOldCode}
                    disabled={sendingOldCode}
                  >
                    {sendingOldCode
                      ? t('Sending...')
                      : oldCodeSent
                        ? t('Resend')
                        : t('Send')}
                  </Button>
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'To protect your account, confirm the change from {{email}} as well.',
                    { email: currentEmail }
                  )}
                </p>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => handleOpenChange(false)}
              disabled={loading}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              onClick={handleBind}
              disabled={
                loading || !email || !code || (needsOldCode && !oldCode)
              }
            >
              {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {loading ? t('Binding...') : t('Bind Email')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
