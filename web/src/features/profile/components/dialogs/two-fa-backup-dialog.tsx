import { useState } from 'react'
import { RefreshCw, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { regenerate2FABackupCodes } from '@/lib/api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { CopyButton } from '@/components/copy-button'
import {
  SecureVerificationDialog,
  useSecureVerification,
  type SecurityProof,
} from '@/features/auth/secure-verification'

// ============================================================================
// Two-FA Backup Codes Dialog Component
// ============================================================================

interface TwoFABackupDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  onSuccess: () => void
}

export function TwoFABackupDialog({
  open,
  onOpenChange,
  onSuccess,
}: TwoFABackupDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [backupCodes, setBackupCodes] = useState<string[]>([])
  const verification = useSecureVerification()

  const handleRegenerate = async () => {
    const submit = async (proof?: SecurityProof) => {
      const response = await regenerate2FABackupCodes(proof?.proof_token)
      if (response.success && response.data?.backup_codes) {
        setBackupCodes(response.data.backup_codes)
        toast.success(t('Backup codes regenerated successfully'))
      } else {
        toast.error(response.message || t('Failed to regenerate backup codes'))
      }
    }

    try {
      setLoading(true)
      await verification.startVerification(submit, {
        operation: { scope: '2fa.backup_codes.regenerate' },
        preferredMethod: '2fa',
        description: t(
          'Confirm with your authenticator code; existing backup codes cannot be used here.'
        ),
      })
    } catch (_error) {
      toast.error(t('Failed to regenerate backup codes'))
    } finally {
      setLoading(false)
    }
  }

  const handleDone = () => {
    handleOpenChange(false)
    onSuccess()
  }

  const handleOpenChange = (open: boolean) => {
    if (!loading) {
      if (!open) {
        setBackupCodes([])
      }
      onOpenChange(open)
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <RefreshCw className='h-5 w-5' />
            {t('Regenerate Backup Codes')}
          </DialogTitle>
          <DialogDescription>
            {backupCodes.length > 0
              ? t('Your new backup codes are ready')
              : t('Generate new backup codes for account recovery')}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-4'>
          {backupCodes.length === 0 ? (
            <>
              <Alert>
                <AlertDescription>
                  {t(
                    'Generating new codes will invalidate all existing backup codes.'
                  )}
                </AlertDescription>
              </Alert>
            </>
          ) : (
            <>
              <Alert>
                <AlertDescription>
                  {t(
                    'Save these codes in a safe place. Each code can only be used once.'
                  )}
                </AlertDescription>
              </Alert>

              <div className='rounded-lg border p-4'>
                <div className='grid grid-cols-2 gap-2'>
                  {backupCodes.map((code, index) => (
                    <div
                      key={index}
                      className='bg-muted rounded-md p-2 text-center font-mono text-sm'
                    >
                      {code}
                    </div>
                  ))}
                </div>
              </div>

              <CopyButton
                value={backupCodes.join('\n')}
                variant='outline'
                size='default'
                className='w-full'
                iconClassName='mr-2 size-4'
                tooltip={t('Copy all backup codes')}
                aria-label={t('Copy all backup codes')}
              >
                {t('Copy All Codes')}
              </CopyButton>
            </>
          )}
        </div>

        <DialogFooter>
          {backupCodes.length === 0 ? (
            <>
              <Button
                variant='outline'
                onClick={() => handleOpenChange(false)}
                disabled={loading}
              >
                {t('Cancel')}
              </Button>
              <Button onClick={handleRegenerate} disabled={loading}>
                {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
                {loading ? t('Generating...') : t('Generate New Codes')}
              </Button>
            </>
          ) : (
            <Button onClick={handleDone}>{t('Done')}</Button>
          )}
        </DialogFooter>
      </DialogContent>
      <SecureVerificationDialog {...verification.dialogProps} />
    </Dialog>
  )
}
