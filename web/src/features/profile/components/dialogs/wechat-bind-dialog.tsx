import { useState } from 'react'
import { Loader2, QrCode } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { bindWeChat } from '../../api'

// ============================================================================
// WeChat Bind Dialog Component
// ============================================================================

interface WeChatBindDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  qrCodeUrl?: string
  onSuccess: () => void
}

export function WeChatBindDialog({
  open,
  onOpenChange,
  qrCodeUrl,
  onSuccess,
}: WeChatBindDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [code, setCode] = useState('')
  const verification = useSecureVerification()

  const handleBind = async () => {
    const value = code.trim()
    if (!value) {
      toast.error(t('Please enter the verification code'))
      return
    }

    const submit = async (proof?: SecurityProof) => {
      const response = await bindWeChat(value, proof?.proof_token)
      if (response.success) {
        toast.success(t('WeChat bound successfully!'))
        onOpenChange(false)
        onSuccess()
        setCode('')
      } else {
        toast.error(response.message || t('Failed to bind WeChat'))
      }
    }

    try {
      setLoading(true)
      await verification.startVerification(submit, {
        operation: {
          scope: 'account.binding.bind',
          context: { provider: 'wechat', code: value },
        },
        description: t('Confirm your identity before binding WeChat.'),
      })
    } catch (_error) {
      toast.error(t('Failed to bind WeChat'))
    } finally {
      setLoading(false)
    }
  }

  const handleOpenChange = (open: boolean) => {
    if (!loading) {
      onOpenChange(open)
      if (!open) setCode('')
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-md'>
        <DialogHeader>
          <DialogTitle>{t('Bind WeChat Account')}</DialogTitle>
          <DialogDescription>
            {t(
              'Scan the QR code to follow the official account, send "验证码" to get a code, then enter it below.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='space-y-4 py-4'>
          {qrCodeUrl ? (
            <div className='flex justify-center'>
              <img
                src={qrCodeUrl}
                alt={t('WeChat QR code')}
                className='size-48 rounded-lg border object-contain'
              />
            </div>
          ) : (
            <div className='flex flex-col items-center justify-center rounded-lg border border-dashed p-8'>
              <QrCode className='text-muted-foreground mb-3 h-16 w-16' />
              <p className='text-muted-foreground text-sm'>
                {t('WeChat QR code is not configured')}
              </p>
            </div>
          )}

          <div className='space-y-2'>
            <Label htmlFor='wechat-code'>{t('Verification Code')}</Label>
            <Input
              id='wechat-code'
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder={t('Enter code')}
              autoComplete='one-time-code'
              disabled={loading}
              maxLength={128}
            />
          </div>
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
            disabled={loading || !code.trim()}
          >
            {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
            {loading ? t('Binding...') : t('Bind')}
          </Button>
        </DialogFooter>
      </DialogContent>
      <SecureVerificationDialog {...verification.dialogProps} />
    </Dialog>
  )
}
