import { useEffect, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOptions } from '../hooks/use-update-option'
import { removeTrailingSlash } from './utils'

export interface WaffoPancakeSettingsValues {
  WaffoPancakeMerchantID: string
  WaffoPancakePrivateKey: string
  WaffoPancakeStoreID: string
  WaffoPancakeProductID: string
  WaffoPancakeReturnURL: string
  WaffoPancakeUnitPrice: number
  WaffoPancakeMinTopUp: number
}

interface Props {
  defaultValues: WaffoPancakeSettingsValues
}

export function WaffoPancakeSettingsSection(props: Props) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()
  const [loading, setLoading] = useState(false)
  const form = useForm<WaffoPancakeSettingsValues>({
    defaultValues: props.defaultValues,
  })

  useEffect(() => {
    form.reset(props.defaultValues)
  }, [props.defaultValues, form])

  const handleSave = async () => {
    const values = form.getValues()

    if (!values.WaffoPancakeMerchantID.trim()) {
      toast.error(t('Merchant ID is required'))
      return
    }

    if (!values.WaffoPancakeStoreID.trim()) {
      toast.error(t('Store ID is required'))
      return
    }

    if (!values.WaffoPancakeProductID.trim()) {
      toast.error(t('Product ID is required'))
      return
    }

    if (Number(values.WaffoPancakeUnitPrice) <= 0) {
      toast.error(t('Unit price must be greater than 0'))
      return
    }

    if (Number(values.WaffoPancakeMinTopUp) < 1) {
      toast.error(t('Minimum top-up amount must be at least 1'))
      return
    }

    setLoading(true)
    try {
      const options: { key: string; value: string }[] = [
        {
          key: 'WaffoPancakeMerchantID',
          value: values.WaffoPancakeMerchantID || '',
        },
        {
          key: 'WaffoPancakeStoreID',
          value: values.WaffoPancakeStoreID || '',
        },
        {
          key: 'WaffoPancakeProductID',
          value: values.WaffoPancakeProductID || '',
        },
        {
          key: 'WaffoPancakeReturnURL',
          value: removeTrailingSlash(values.WaffoPancakeReturnURL || ''),
        },
        {
          key: 'WaffoPancakeUnitPrice',
          value: String(values.WaffoPancakeUnitPrice ?? 1),
        },
        {
          key: 'WaffoPancakeMinTopUp',
          value: String(values.WaffoPancakeMinTopUp ?? 1),
        },
      ]

      if ((values.WaffoPancakePrivateKey || '').trim()) {
        options.push({
          key: 'WaffoPancakePrivateKey',
          value: values.WaffoPancakePrivateKey,
        })
      }

      await updateOptions.mutateAsync(options)
    } catch {
      // The mutation owns request error feedback.
    } finally {
      setLoading(false)
    }
  }

  return (
    <SettingsSection
      title={t('Waffo Pancake Payment Gateway')}
      description={t(
        'Configure Waffo Pancake hosted checkout integration for USD-priced top-ups'
      )}
    >
      <Alert>
        <AlertDescription className='text-xs'>
          {t('Webhook URL')}:{' '}
          <code>&lt;ServerAddress&gt;/api/waffo-pancake/webhook/prod</code>
          {' / '}
          <code>&lt;ServerAddress&gt;/api/waffo-pancake/webhook/test</code>
        </AlertDescription>
      </Alert>

      <div className='grid grid-cols-3 gap-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Merchant ID')}</Label>
          <Input
            placeholder='MER_xxx'
            {...form.register('WaffoPancakeMerchantID')}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Store ID')}</Label>
          <Input
            placeholder='STO_xxx'
            {...form.register('WaffoPancakeStoreID')}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Product ID')}</Label>
          <Input
            placeholder='PROD_xxx'
            {...form.register('WaffoPancakeProductID')}
          />
        </div>
      </div>

      <div className='grid grid-cols-2 gap-4'>
        <div className='grid gap-1.5'>
          <Label>{t('API Private Key')}</Label>
          <Textarea
            rows={3}
            placeholder={t('Leave blank to keep the existing key')}
            {...form.register('WaffoPancakePrivateKey')}
            className='font-mono text-xs'
          />
          <p className='text-muted-foreground text-xs'>
            {t('Stored value is not echoed back for security')}
          </p>
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Payment return URL')}</Label>
          <Input
            placeholder='https://example.com/console/topup'
            {...form.register('WaffoPancakeReturnURL')}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Defaults to the wallet page when empty')}
          </p>
        </div>
      </div>

      <div className='grid grid-cols-2 gap-4'>
        <div className='grid gap-1.5'>
          <Label>{t('Unit price (local currency / USD)')}</Label>
          <Input
            type='number'
            step={0.01}
            min={0}
            {...form.register('WaffoPancakeUnitPrice', { valueAsNumber: true })}
          />
        </div>
        <div className='grid gap-1.5'>
          <Label>{t('Minimum top-up (USD)')}</Label>
          <Input
            type='number'
            min={1}
            {...form.register('WaffoPancakeMinTopUp', { valueAsNumber: true })}
          />
        </div>
      </div>

      <Button onClick={handleSave} disabled={loading}>
        {loading ? t('Saving...') : t('Save Waffo Pancake settings')}
      </Button>
    </SettingsSection>
  )
}
