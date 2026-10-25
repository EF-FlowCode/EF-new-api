import { useState } from 'react'
import { AlertTriangle, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { disable2FA } from '@/lib/api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import {
  SecureVerificationDialog,
  useSecureVerification,
  type SecurityProof,
} from '@/features/auth/secure-verification'

// ============================================================================
// Two-FA Disable Dialog Component
// ============================================================================

interface TwoFADisableDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function TwoFADisableDialog({
  open,
  onOpenChange,
  onSuccess,
}: TwoFADisableDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const verification = useSecureVerification()

  const handleDisable = async () => {
    if (!confirmed) {
      toast.error(t('Please confirm that you understand the consequences'))
      return
    }

    const submit = async (proof?: SecurityProof) => {
      const response = await disable2FA(proof?.proof_token)
      if (response.success) {
        toast.success(t('Two-factor authentication disabled'))
        onOpenChange(false)
        onSuccess()
        setConfirmed(false)
      } else {
        toast.error(response.message || t('Failed to disable 2FA'))
      }
    }

    try {
      setLoading(true)
      await verification.startVerification(submit, {
        operation: { scope: '2fa.disable' },
        preferredMethod: '2fa',
        description: t(
          'Confirm your identity before disabling two-factor authentication.'
        ),
      })
    } catch (_error) {
      toast.error(t('Failed to disable 2FA'))
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean) => {
    if (!loading) {
      if (!open) {
        setConfirmed(false)
      }
      onOpenChange(open)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='text-destructive flex items-center gap-2'>
            <AlertTriangle className='h-5 w-5' />
            {t('Disable Two-Factor Authentication')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'This action will permanently remove 2FA protection from your account.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-4'>
          <Alert variant='destructive'>
            <AlertTriangle className='h-4 w-4' />
            <AlertDescription>
              {t('Warning: Disabling 2FA will make your account less secure.')}
            </AlertDescription>
          </Alert>

          <div className='flex items-start space-x-2'>
            <Checkbox
              id='confirm'
              checked={confirmed}
              onCheckedChange={(checked) => setConfirmed(checked as boolean)}
            />
            <Label
              htmlFor='confirm'
              className='text-sm leading-tight font-normal'
            >
              {t(
                'I understand that disabling 2FA will remove all protection and backup codes'
              )}
            </Label>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => handleOpenChange(false)}
            disabled={loading}
          >
            {t('Cancel')}
          </Button>
          <Button
            variant='destructive'
            onClick={handleDisable}
            disabled={loading || !confirmed}
          >
            {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {loading ? t('Disabling...') : t('Disable 2FA')}
          </Button>
        </DialogFooter>
      </DialogContent>
      <SecureVerificationDialog {...verification.dialogProps} />
    </Dialog>
  )
}
