import * as React from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { ShieldAlert, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { confirmPaymentCompliance } from '../api'

// eflow: 上游 rc 给支付/兑换码/邀请返利加了"合规门禁"——确认前后端直接拒绝
// （兑换码生成会报 "支付、兑换码和邀请返利功能已禁用"）。但上游的"确认合规
// 声明" UI 曾缺少入口，导致后台无处点确认、功能被永久锁死。这里提供确认入口：
// 勾选声明 → POST /api/option/payment_compliance → 后端写 option 解锁。
// 必须与后端 operation_setting.CurrentComplianceTermsVersion 保持一致。
const CURRENT_COMPLIANCE_TERMS_VERSION = 'v1'

type PaymentComplianceCardProps = {
  confirmed: boolean
  confirmedAt?: number
  termsVersion?: string
}

export function PaymentComplianceCard({
  confirmed,
  confirmedAt,
  termsVersion,
}: PaymentComplianceCardProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [agreed, setAgreed] = React.useState(false)

  const isConfirmed =
    confirmed && termsVersion === CURRENT_COMPLIANCE_TERMS_VERSION

  const mutation = useMutation({
    mutationFn: confirmPaymentCompliance,
    onSuccess: (data) => {
      if (data?.success) {
        queryClient.invalidateQueries({ queryKey: ['system-options'] })
        toast.success(t('Compliance statement confirmed. Features enabled.'))
      } else if (data?.message) {
        toast.error(data.message)
      }
    },
  })

  if (isConfirmed) {
    const when =
      confirmedAt && confirmedAt > 0
        ? new Date(confirmedAt * 1000).toLocaleString()
        : null
    return (
      <Alert className='border-green-500/40'>
        <ShieldCheck className='text-green-600' />
        <AlertTitle>{t('Payment compliance confirmed')}</AlertTitle>
        <AlertDescription>
          {t('Payment, redemption codes and affiliate rewards are enabled.')}
          {when ? ` ${t('Confirmed at')}: ${when}` : null}
        </AlertDescription>
      </Alert>
    )
  }

  return (
    <Alert variant='destructive' className='border-destructive/40'>
      <ShieldAlert />
      <AlertTitle>{t('Compliance confirmation required')}</AlertTitle>
      <AlertDescription>
        <p>
          {t(
            'Payment, redemption codes and affiliate rewards are disabled until the administrator confirms the compliance statement.'
          )}
        </p>
        <label className='text-foreground mt-2 flex items-start gap-2'>
          <Checkbox
            checked={agreed}
            onCheckedChange={(value) => setAgreed(value === true)}
            className='mt-0.5'
          />
          <span>
            {t(
              'I have read and accept the compliance statement, and take responsibility for the legality of payment-related features on this platform.'
            )}
          </span>
        </label>
        <Button
          type='button'
          className='mt-3'
          disabled={!agreed || mutation.isPending}
          onClick={() => mutation.mutate()}
        >
          {mutation.isPending
            ? t('Confirming...')
            : t('Confirm and enable features')}
        </Button>
      </AlertDescription>
    </Alert>
  )
}
