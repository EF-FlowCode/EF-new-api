import * as React from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import {
  CircleDollarSign,
  Code2,
  CreditCard,
  Eye,
  Settings2,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOptions } from '../hooks/use-update-option'
import { safeJsonParseWithValidation } from '../utils/json-parser'
import { AmountOptionsVisualEditor } from './amount-options-visual-editor'
import { CreemProductsVisualEditor } from './creem-products-visual-editor'
import { PaymentComplianceCard } from './payment-compliance-card'
import { PaymentMethodsVisualEditor } from './payment-methods-visual-editor'
import {
  formatJsonForEditor,
  getJsonError,
  normalizeJsonForComparison,
  removeTrailingSlash,
} from './utils'
import {
  WaffoPancakeSettingsSection,
  type WaffoPancakeSettingsValues,
} from './waffo-pancake-settings-section'
import {
  WaffoSettingsSection,
  type WaffoSettingsValues,
} from './waffo-settings-section'

const paymentSchema = z.object({
  PayAddress: z.string().refine((value) => {
    const trimmed = value.trim()
    if (!trimmed) return true
    return /^https?:\/\//.test(trimmed)
  }, 'Provide a valid callback URL starting with http:// or https://'),
  EpayId: z.string(),
  EpayKey: z.string(),
  Price: z.coerce.number().min(0),
  MinTopUp: z.coerce.number().min(0),
  CustomCallbackAddress: z.string().refine((value) => {
    const trimmed = value.trim()
    if (!trimmed) return true
    return /^https?:\/\//.test(trimmed)
  }, 'Provide a valid URL starting with http:// or https://'),
  PayMethods: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value)
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  AmountOptions: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value, (parsed) => Array.isArray(parsed))
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  AmountDiscount: z.string().superRefine((value, ctx) => {
    const error = getJsonError(
      value,
      (parsed) =>
        !!parsed && typeof parsed === 'object' && !Array.isArray(parsed)
    )
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
  StripeApiSecret: z.string(),
  StripeWebhookSecret: z.string(),
  StripePriceId: z.string(),
  StripeUnitPrice: z.coerce.number().min(0),
  StripeMinTopUp: z.coerce.number().min(0),
  StripePromotionCodesEnabled: z.boolean(),
  CreemApiKey: z.string(),
  CreemWebhookSecret: z.string(),
  CreemTestMode: z.boolean(),
  CreemProducts: z.string().superRefine((value, ctx) => {
    const error = getJsonError(value, (parsed) => Array.isArray(parsed))
    if (error) {
      ctx.addIssue({
        code: z.ZodIssueCode.custom,
        message: error,
      })
    }
  }),
})

type PaymentFormValues = z.infer<typeof paymentSchema>

type PaymentSettingsSectionProps = {
  defaultValues: PaymentFormValues
  waffoDefaultValues: WaffoSettingsValues
  waffoPancakeDefaultValues: WaffoPancakeSettingsValues
  complianceConfirmed: boolean
  complianceConfirmedAt: number
  complianceTermsVersion: string
}

type PaymentProvider =
  | 'general'
  | 'epay'
  | 'stripe'
  | 'creem'
  | 'waffo'
  | 'waffo-pancake'

export function PaymentSettingsSection({
  defaultValues,
  waffoDefaultValues,
  waffoPancakeDefaultValues,
  complianceConfirmed,
  complianceConfirmedAt,
  complianceTermsVersion,
}: PaymentSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOptions = useUpdateOptions()
  const initialRef = React.useRef(defaultValues)
  const defaultsSignature = React.useMemo(
    () => JSON.stringify(defaultValues),
    [defaultValues]
  )

  const [payMethodsVisualMode, setPayMethodsVisualMode] = React.useState(true)
  const [amountOptionsVisualMode, setAmountOptionsVisualMode] =
    React.useState(true)
  const [creemProductsVisualMode, setCreemProductsVisualMode] =
    React.useState(true)
  const [activeProvider, setActiveProvider] =
    React.useState<PaymentProvider>('general')
  const [providerSheetOpen, setProviderSheetOpen] = React.useState(false)

  const form = useForm({
    resolver: zodResolver(paymentSchema),
    mode: 'onChange', // Enable real-time validation
    defaultValues: {
      ...defaultValues,
      PayMethods: formatJsonForEditor(defaultValues.PayMethods),
      AmountOptions: formatJsonForEditor(defaultValues.AmountOptions),
      AmountDiscount: formatJsonForEditor(defaultValues.AmountDiscount),
      CreemProducts: formatJsonForEditor(defaultValues.CreemProducts),
    },
  })

  React.useEffect(() => {
    const parsedDefaults = JSON.parse(defaultsSignature) as PaymentFormValues
    initialRef.current = parsedDefaults
    form.reset({
      ...parsedDefaults,
      PayMethods: formatJsonForEditor(parsedDefaults.PayMethods),
      AmountOptions: formatJsonForEditor(parsedDefaults.AmountOptions),
      AmountDiscount: formatJsonForEditor(parsedDefaults.AmountDiscount),
      CreemProducts: formatJsonForEditor(parsedDefaults.CreemProducts),
    })
  }, [defaultsSignature, form])

  const payMethods = safeJsonParseWithValidation<unknown[]>(
    defaultValues.PayMethods,
    {
      fallback: [],
      validator: Array.isArray,
      silent: true,
    }
  )
  const creemProducts = safeJsonParseWithValidation<unknown[]>(
    defaultValues.CreemProducts,
    {
      fallback: [],
      validator: Array.isArray,
      silent: true,
    }
  )
  const waffoPayMethods = safeJsonParseWithValidation<unknown[]>(
    waffoDefaultValues.WaffoPayMethods,
    {
      fallback: [],
      validator: Array.isArray,
      silent: true,
    }
  )

  const providers = [
    {
      id: 'epay' as const,
      name: 'Epay',
      initials: 'EP',
      active: Boolean(defaultValues.PayAddress && defaultValues.EpayId),
      description: t('Configuration for Epay payment integration'),
      detail: `${t('Payment methods')}: ${payMethods.length}`,
    },
    {
      id: 'stripe' as const,
      name: 'Stripe',
      initials: 'ST',
      active: Boolean(defaultValues.StripePriceId),
      description: t('Configuration for Stripe payment integration'),
      detail: `${t('Price ID')}: ${defaultValues.StripePriceId || '—'}`,
    },
    {
      id: 'creem' as const,
      name: 'Creem',
      initials: 'CR',
      active: creemProducts.length > 0,
      description: t('Configuration for Creem payment integration'),
      detail: `${t('Products')}: ${creemProducts.length}`,
    },
    {
      id: 'waffo' as const,
      name: 'Waffo',
      initials: 'WF',
      active: waffoDefaultValues.WaffoEnabled,
      description: t(
        'Configure Waffo payment aggregation platform integration'
      ),
      detail: `${waffoDefaultValues.WaffoCurrency || 'USD'} · ${t('Payment methods')}: ${waffoPayMethods.length}`,
    },
    {
      id: 'waffo-pancake' as const,
      name: 'Waffo Pancake',
      initials: 'PC',
      active: Boolean(
        waffoPancakeDefaultValues.WaffoPancakeMerchantID &&
        waffoPancakeDefaultValues.WaffoPancakeProductID
      ),
      description: t(
        'Configure Waffo Pancake hosted checkout integration for USD-priced top-ups'
      ),
      detail: `USD · ${t('Minimum top-up (USD)')}: ${waffoPancakeDefaultValues.WaffoPancakeMinTopUp}`,
    },
  ]
  const configuredProviderCount = providers.filter(
    (provider) => provider.active
  ).length
  const activeProviderTitle = {
    general: t('General Settings'),
    epay: t('Epay Gateway'),
    stripe: t('Stripe Gateway'),
    creem: t('Creem Gateway'),
    waffo: t('Waffo Payment Gateway'),
    'waffo-pancake': t('Waffo Pancake Payment Gateway'),
  }[activeProvider]

  const openProvider = (provider: PaymentProvider) => {
    setActiveProvider(provider)
    setProviderSheetOpen(true)
  }

  const saveGeneralSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      Price: values.Price as number,
      MinTopUp: values.MinTopUp as number,
      PayMethods: values.PayMethods.trim(),
      AmountOptions: values.AmountOptions.trim(),
      AmountDiscount: values.AmountDiscount.trim(),
    }

    const initial = {
      Price: initialRef.current.Price,
      MinTopUp: initialRef.current.MinTopUp,
      PayMethods: initialRef.current.PayMethods.trim(),
      AmountOptions: initialRef.current.AmountOptions.trim(),
      AmountDiscount: initialRef.current.AmountDiscount.trim(),
    }

    const updates: Array<{ key: string; value: string | number }> = []

    if (sanitized.Price !== initial.Price) {
      updates.push({ key: 'Price', value: sanitized.Price })
    }

    if (sanitized.MinTopUp !== initial.MinTopUp) {
      updates.push({ key: 'MinTopUp', value: sanitized.MinTopUp })
    }

    if (
      normalizeJsonForComparison(sanitized.PayMethods) !==
      normalizeJsonForComparison(initial.PayMethods)
    ) {
      updates.push({ key: 'PayMethods', value: sanitized.PayMethods })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountOptions) !==
      normalizeJsonForComparison(initial.AmountOptions)
    ) {
      updates.push({
        key: 'payment_setting.amount_options',
        value: sanitized.AmountOptions,
      })
    }

    if (
      normalizeJsonForComparison(sanitized.AmountDiscount) !==
      normalizeJsonForComparison(initial.AmountDiscount)
    ) {
      updates.push({
        key: 'payment_setting.amount_discount',
        value: sanitized.AmountDiscount,
      })
    }

    if (updates.length === 0) {
      return
    }

    await updateOptions.mutateAsync(updates)
  }

  const saveEpaySettings = async () => {
    const values = form.getValues()
    const sanitized = {
      PayAddress: removeTrailingSlash(values.PayAddress),
      EpayId: values.EpayId.trim(),
      EpayKey: values.EpayKey.trim(),
      CustomCallbackAddress: removeTrailingSlash(values.CustomCallbackAddress),
    }

    const initial = {
      PayAddress: removeTrailingSlash(initialRef.current.PayAddress),
      EpayId: initialRef.current.EpayId.trim(),
      EpayKey: initialRef.current.EpayKey.trim(),
      CustomCallbackAddress: removeTrailingSlash(
        initialRef.current.CustomCallbackAddress
      ),
    }

    const updates: Array<{ key: string; value: string }> = []

    if (sanitized.PayAddress !== initial.PayAddress) {
      updates.push({ key: 'PayAddress', value: sanitized.PayAddress })
    }

    if (sanitized.EpayId !== initial.EpayId) {
      updates.push({ key: 'EpayId', value: sanitized.EpayId })
    }

    if (sanitized.EpayKey && sanitized.EpayKey !== initial.EpayKey) {
      updates.push({ key: 'EpayKey', value: sanitized.EpayKey })
    }

    if (sanitized.CustomCallbackAddress !== initial.CustomCallbackAddress) {
      updates.push({
        key: 'CustomCallbackAddress',
        value: sanitized.CustomCallbackAddress,
      })
    }

    if (updates.length === 0) {
      return
    }

    await updateOptions.mutateAsync(updates)
  }

  const saveStripeSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      StripeApiSecret: values.StripeApiSecret.trim(),
      StripeWebhookSecret: values.StripeWebhookSecret.trim(),
      StripePriceId: values.StripePriceId.trim(),
      StripeUnitPrice: values.StripeUnitPrice as number,
      StripeMinTopUp: values.StripeMinTopUp as number,
      StripePromotionCodesEnabled:
        values.StripePromotionCodesEnabled as boolean,
    }

    const initial = {
      StripeApiSecret: initialRef.current.StripeApiSecret.trim(),
      StripeWebhookSecret: initialRef.current.StripeWebhookSecret.trim(),
      StripePriceId: initialRef.current.StripePriceId.trim(),
      StripeUnitPrice: initialRef.current.StripeUnitPrice,
      StripeMinTopUp: initialRef.current.StripeMinTopUp,
      StripePromotionCodesEnabled:
        initialRef.current.StripePromotionCodesEnabled,
    }

    const updates: Array<{ key: string; value: string | number | boolean }> = []

    if (
      sanitized.StripeApiSecret &&
      sanitized.StripeApiSecret !== initial.StripeApiSecret
    ) {
      updates.push({ key: 'StripeApiSecret', value: sanitized.StripeApiSecret })
    }

    if (
      sanitized.StripeWebhookSecret &&
      sanitized.StripeWebhookSecret !== initial.StripeWebhookSecret
    ) {
      updates.push({
        key: 'StripeWebhookSecret',
        value: sanitized.StripeWebhookSecret,
      })
    }

    if (sanitized.StripePriceId !== initial.StripePriceId) {
      updates.push({ key: 'StripePriceId', value: sanitized.StripePriceId })
    }

    if (sanitized.StripeUnitPrice !== initial.StripeUnitPrice) {
      updates.push({ key: 'StripeUnitPrice', value: sanitized.StripeUnitPrice })
    }

    if (sanitized.StripeMinTopUp !== initial.StripeMinTopUp) {
      updates.push({ key: 'StripeMinTopUp', value: sanitized.StripeMinTopUp })
    }

    if (
      sanitized.StripePromotionCodesEnabled !==
      initial.StripePromotionCodesEnabled
    ) {
      updates.push({
        key: 'StripePromotionCodesEnabled',
        value: sanitized.StripePromotionCodesEnabled,
      })
    }

    if (updates.length === 0) {
      return
    }

    await updateOptions.mutateAsync(updates)
  }

  const saveCreemSettings = async () => {
    const values = form.getValues()
    const sanitized = {
      CreemApiKey: values.CreemApiKey.trim(),
      CreemWebhookSecret: values.CreemWebhookSecret.trim(),
      CreemTestMode: values.CreemTestMode as boolean,
      CreemProducts: values.CreemProducts.trim(),
    }

    const initial = {
      CreemApiKey: initialRef.current.CreemApiKey.trim(),
      CreemWebhookSecret: initialRef.current.CreemWebhookSecret.trim(),
      CreemTestMode: initialRef.current.CreemTestMode,
      CreemProducts: initialRef.current.CreemProducts.trim(),
    }

    const updates: Array<{ key: string; value: string | boolean }> = []

    if (
      sanitized.CreemApiKey &&
      sanitized.CreemApiKey !== initial.CreemApiKey
    ) {
      updates.push({ key: 'CreemApiKey', value: sanitized.CreemApiKey })
    }

    if (
      sanitized.CreemWebhookSecret &&
      sanitized.CreemWebhookSecret !== initial.CreemWebhookSecret
    ) {
      updates.push({
        key: 'CreemWebhookSecret',
        value: sanitized.CreemWebhookSecret,
      })
    }

    if (sanitized.CreemTestMode !== initial.CreemTestMode) {
      updates.push({ key: 'CreemTestMode', value: sanitized.CreemTestMode })
    }

    if (
      normalizeJsonForComparison(sanitized.CreemProducts) !==
      normalizeJsonForComparison(initial.CreemProducts)
    ) {
      updates.push({ key: 'CreemProducts', value: sanitized.CreemProducts })
    }

    if (updates.length === 0) {
      return
    }

    await updateOptions.mutateAsync(updates)
  }

  return (
    <SettingsSection
      title={t('Payment Gateway')}
      description={t(
        'Configure recharge pricing and payment gateway integrations'
      )}
    >
      {/* eflow: 支付合规门禁确认入口（确认前支付/兑换码/邀请返利被后端禁用） */}
      <PaymentComplianceCard
        confirmed={complianceConfirmed}
        confirmedAt={complianceConfirmedAt}
        termsVersion={complianceTermsVersion}
      />

      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3'>
        <Card className='gap-3 py-4 shadow-none'>
          <CardHeader className='flex-row items-center justify-between px-4'>
            <CardDescription>{t('Payment Gateway')}</CardDescription>
            <CreditCard className='text-muted-foreground size-4' />
          </CardHeader>
          <CardContent className='px-4'>
            <p className='text-2xl font-semibold'>
              {configuredProviderCount} / {providers.length}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Available')}
            </p>
          </CardContent>
        </Card>

        <Card className='gap-3 py-4 shadow-none'>
          <CardHeader className='flex-row items-center justify-between px-4'>
            <CardDescription>
              {t('Price (local currency / USD)')}
            </CardDescription>
            <CircleDollarSign className='text-muted-foreground size-4' />
          </CardHeader>
          <CardContent className='px-4'>
            <p className='text-2xl font-semibold'>{defaultValues.Price}</p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t('Shared configuration for all payment gateways')}
            </p>
          </CardContent>
        </Card>

        <Card className='col-span-2 gap-3 py-4 shadow-none sm:col-span-1'>
          <CardHeader className='flex-row items-center justify-between px-4'>
            <CardDescription>{t('Minimum top-up (USD)')}</CardDescription>
            <Settings2 className='text-muted-foreground size-4' />
          </CardHeader>
          <CardContent className='flex items-end justify-between gap-3 px-4'>
            <div>
              <p className='text-2xl font-semibold'>{defaultValues.MinTopUp}</p>
              <p className='text-muted-foreground mt-1 text-xs'>
                {t('General Settings')}
              </p>
            </div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              data-provider='general'
              onClick={() => openProvider('general')}
            >
              {t('Configure')}
            </Button>
          </CardContent>
        </Card>
      </div>

      <div className='space-y-3'>
        <div>
          <h3 className='text-base font-semibold'>{t('Payment Gateway')}</h3>
          <p className='text-muted-foreground text-sm'>
            {t('Configure recharge pricing and payment gateway integrations')}
          </p>
        </div>
        <div className='grid gap-3 sm:grid-cols-2 xl:grid-cols-3'>
          {providers.map((provider) => (
            <Card key={provider.id} className='gap-4 py-5 shadow-none'>
              <CardHeader className='gap-3 px-5'>
                <div className='flex items-center justify-between gap-3'>
                  <span className='bg-muted flex size-9 items-center justify-center rounded-lg border text-xs font-semibold'>
                    {provider.initials}
                  </span>
                  <Badge
                    variant={provider.active ? 'default' : 'outline'}
                    className='gap-1.5'
                  >
                    <span
                      className={`size-1.5 rounded-full ${provider.active ? 'bg-primary-foreground' : 'bg-muted-foreground'}`}
                    />
                    {provider.active ? t('Active') : t('Configure')}
                  </Badge>
                </div>
                <div className='space-y-1'>
                  <CardTitle>{provider.name}</CardTitle>
                  <CardDescription className='line-clamp-2 min-h-10'>
                    {provider.description}
                  </CardDescription>
                </div>
              </CardHeader>
              <CardContent className='text-muted-foreground px-5 text-xs'>
                {provider.detail}
              </CardContent>
              <CardFooter className='px-5'>
                <Button
                  type='button'
                  variant='outline'
                  className='w-full'
                  data-provider={provider.id}
                  onClick={() => openProvider(provider.id)}
                >
                  <Settings2 />
                  {t('Configure')}
                </Button>
              </CardFooter>
            </Card>
          ))}
        </div>
      </div>

      <Sheet open={providerSheetOpen} onOpenChange={setProviderSheetOpen}>
        <SheetContent className='flex h-dvh w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl'>
          <SheetHeader className='border-b px-4 py-3 text-start sm:px-6 sm:py-4'>
            <SheetTitle>{activeProviderTitle}</SheetTitle>
            <SheetDescription>
              {t('Configure recharge pricing and payment gateway integrations')}
            </SheetDescription>
          </SheetHeader>
          <div className='flex-1 overflow-y-auto px-4 py-5 sm:px-6'>
            <Form {...form}>
              <form
                onSubmit={(event) => event.preventDefault()}
                data-no-autosubmit='true'
              >
                <div
                  className={
                    activeProvider === 'general' ? 'space-y-4' : 'hidden'
                  }
                >
                  <div>
                    <h3 className='text-lg font-medium'>
                      {t('General Settings')}
                    </h3>
                    <p className='text-muted-foreground text-sm'>
                      {t('Shared configuration for all payment gateways')}
                    </p>
                  </div>

                  <div className='grid gap-6 md:grid-cols-2'>
                    <FormField
                      control={form.control}
                      name='Price'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {t('Price (local currency / USD)')}
                          </FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              step='0.01'
                              min={0}
                              value={(field.value ?? 0) as number}
                              onChange={(event) =>
                                field.onChange(event.target.valueAsNumber)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'How much to charge for each US dollar of balance (Epay)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='MinTopUp'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Minimum top-up (USD)')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              step='0.01'
                              min={0}
                              value={(field.value ?? 0) as number}
                              onChange={(event) =>
                                field.onChange(event.target.valueAsNumber)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Smallest USD amount users can recharge (Epay)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <FormField
                    control={form.control}
                    name='PayMethods'
                    render={({ field }) => (
                      <FormItem>
                        <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                          <FormLabel>{t('Payment methods')}</FormLabel>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() =>
                              setPayMethodsVisualMode(!payMethodsVisualMode)
                            }
                            className='w-full sm:w-auto'
                          >
                            {payMethodsVisualMode ? (
                              <>
                                <Code2 className='mr-2 h-3 w-3' />
                                {t('JSON Editor')}
                              </>
                            ) : (
                              <>
                                <Eye className='mr-2 h-3 w-3' />
                                {t('Visual Editor')}
                              </>
                            )}
                          </Button>
                        </div>
                        <FormControl>
                          {payMethodsVisualMode ? (
                            <PaymentMethodsVisualEditor
                              value={field.value}
                              onChange={field.onChange}
                            />
                          ) : (
                            <JsonCodeEditor
                              value={field.value}
                              onChange={field.onChange}
                              name={field.name}
                              onBlur={field.onBlur}
                              textareaRef={field.ref}
                              placeholder={t(
                                '[{"name":"支付宝","type":"alipay","color":"#1677FF"}]'
                              )}
                              heightClassName='h-40 min-h-40 max-h-40'
                              aria-invalid={Boolean(
                                form.formState.errors.PayMethods
                              )}
                            />
                          )}
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Configure available payment methods. Provide a JSON array.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <div>
                    <FormField
                      control={form.control}
                      name='AmountOptions'
                      render={({ field }) => (
                        <FormItem>
                          <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                            <FormLabel>{t('Top-up amount options')}</FormLabel>
                            <Button
                              type='button'
                              variant='outline'
                              size='sm'
                              onClick={() =>
                                setAmountOptionsVisualMode(
                                  !amountOptionsVisualMode
                                )
                              }
                              className='w-full sm:w-auto'
                            >
                              {amountOptionsVisualMode ? (
                                <>
                                  <Code2 className='mr-2 h-3 w-3' />
                                  {t('JSON Editor')}
                                </>
                              ) : (
                                <>
                                  <Eye className='mr-2 h-3 w-3' />
                                  {t('Visual Editor')}
                                </>
                              )}
                            </Button>
                          </div>
                          <FormControl>
                            {amountOptionsVisualMode ? (
                              <AmountOptionsVisualEditor
                                value={field.value}
                                onChange={field.onChange}
                              />
                            ) : (
                              <JsonCodeEditor
                                value={field.value}
                                onChange={field.onChange}
                                name={field.name}
                                onBlur={field.onBlur}
                                textareaRef={field.ref}
                                placeholder='[10, 20, 50, 100]'
                                heightClassName='h-40 min-h-40 max-h-40'
                                aria-invalid={Boolean(
                                  form.formState.errors.AmountOptions
                                )}
                              />
                            )}
                          </FormControl>
                          <FormDescription>
                            {t('Preset recharge amounts (JSON array)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <Button
                    type='button'
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      saveGeneralSettings()
                    }}
                    disabled={updateOptions.isPending}
                  >
                    {updateOptions.isPending
                      ? t('Saving...')
                      : t('Save general settings')}
                  </Button>
                </div>

                <div
                  className={activeProvider === 'epay' ? 'space-y-4' : 'hidden'}
                >
                  <div>
                    <h3 className='text-lg font-medium'>{t('Epay Gateway')}</h3>
                    <p className='text-muted-foreground text-sm'>
                      {t('Configuration for Epay payment integration')}
                    </p>
                  </div>

                  <div className='grid gap-6 md:grid-cols-2'>
                    <FormField
                      control={form.control}
                      name='PayAddress'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Epay endpoint')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder={t('https://pay.example.com')}
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Base address provided by your Epay service')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='CustomCallbackAddress'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Callback address')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder={t('https://gateway.example.com')}
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Optional callback override. Leave blank to use server address'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <div className='grid gap-6 md:grid-cols-2'>
                    <FormField
                      control={form.control}
                      name='EpayId'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Epay merchant ID')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder='10001'
                              autoComplete='off'
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='EpayKey'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Epay secret key')}</FormLabel>
                          <FormControl>
                            <Input
                              type='password'
                              placeholder={t('Enter new key to update')}
                              autoComplete='new-password'
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Leave blank unless rotating the secret')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <Button
                    type='button'
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      saveEpaySettings()
                    }}
                    disabled={updateOptions.isPending}
                  >
                    {updateOptions.isPending
                      ? t('Saving...')
                      : t('Save Epay settings')}
                  </Button>
                </div>

                <div
                  className={
                    activeProvider === 'stripe' ? 'space-y-4' : 'hidden'
                  }
                >
                  <div>
                    <h3 className='text-lg font-medium'>
                      {t('Stripe Gateway')}
                    </h3>
                    <p className='text-muted-foreground text-sm'>
                      {t('Configuration for Stripe payment integration')}
                    </p>
                  </div>

                  <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
                    <p className='mb-2 font-medium'>
                      {t('Webhook Configuration:')}
                    </p>
                    <ul className='list-inside list-disc space-y-1'>
                      <li>
                        {t('Webhook URL:')}{' '}
                        <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                          {'<ServerAddress>/api/stripe/webhook'}
                        </code>
                      </li>
                      <li>
                        {t('Required events:')}{' '}
                        <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                          {t('checkout.session.completed')}
                        </code>{' '}
                        {t('and')}{' '}
                        <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                          {t('checkout.session.expired')}
                        </code>
                      </li>
                      <li>
                        {t('Configure at:')}{' '}
                        <a
                          href='https://dashboard.stripe.com/developers'
                          target='_blank'
                          rel='noreferrer'
                          className='underline hover:no-underline'
                        >
                          {t('Stripe Dashboard')}
                        </a>
                      </li>
                    </ul>
                  </div>

                  <div className='grid gap-6 md:grid-cols-3'>
                    <FormField
                      control={form.control}
                      name='StripeApiSecret'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('API secret')}</FormLabel>
                          <FormControl>
                            <Input
                              type='password'
                              placeholder={t('sk_xxx or rk_xxx')}
                              autoComplete='new-password'
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Stripe API key (leave blank unless updating)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='StripeWebhookSecret'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Webhook secret')}</FormLabel>
                          <FormControl>
                            <Input
                              type='password'
                              placeholder={t('whsec_xxx')}
                              autoComplete='new-password'
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Webhook signing secret (leave blank unless updating)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='StripePriceId'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Price ID')}</FormLabel>
                          <FormControl>
                            <Input
                              placeholder={t('price_xxx')}
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Stripe product price ID')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <div className='grid gap-6 md:grid-cols-3'>
                    <FormField
                      control={form.control}
                      name='StripeUnitPrice'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>
                            {t('Unit price (local currency / USD)')}
                          </FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              step='0.01'
                              min={0}
                              value={(field.value ?? 0) as number}
                              onChange={(event) =>
                                field.onChange(event.target.valueAsNumber)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('e.g., 8 means 8 local currency per USD')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='StripeMinTopUp'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Minimum top-up (USD)')}</FormLabel>
                          <FormControl>
                            <Input
                              type='number'
                              step='0.01'
                              min={0}
                              value={(field.value ?? 0) as number}
                              onChange={(event) =>
                                field.onChange(event.target.valueAsNumber)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Minimum recharge amount in USD')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='StripePromotionCodesEnabled'
                      render={({ field }) => (
                        <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                          <div className='space-y-0.5'>
                            <FormLabel className='text-base'>
                              {t('Promotion codes')}
                            </FormLabel>
                            <FormDescription>
                              {t('Allow users to enter promo codes')}
                            </FormDescription>
                          </div>
                          <FormControl>
                            <Switch
                              checked={field.value}
                              onCheckedChange={field.onChange}
                            />
                          </FormControl>
                        </FormItem>
                      )}
                    />
                  </div>

                  <Button
                    type='button'
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      saveStripeSettings()
                    }}
                    disabled={updateOptions.isPending}
                  >
                    {updateOptions.isPending
                      ? t('Saving...')
                      : t('Save Stripe settings')}
                  </Button>
                </div>

                <div
                  className={
                    activeProvider === 'creem' ? 'space-y-4' : 'hidden'
                  }
                >
                  <div>
                    <h3 className='text-lg font-medium'>
                      {t('Creem Gateway')}
                    </h3>
                    <p className='text-muted-foreground text-sm'>
                      {t('Configuration for Creem payment integration')}
                    </p>
                  </div>

                  <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
                    <p className='mb-2 font-medium'>
                      {t('Webhook Configuration:')}
                    </p>
                    <ul className='list-inside list-disc space-y-1'>
                      <li>
                        {t('Webhook URL:')}{' '}
                        <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                          {'<ServerAddress>/api/creem/webhook'}
                        </code>
                      </li>
                      <li>{t('Configure in your Creem dashboard')}</li>
                    </ul>
                  </div>

                  <div className='grid gap-6 md:grid-cols-2'>
                    <FormField
                      control={form.control}
                      name='CreemApiKey'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('API Key')}</FormLabel>
                          <FormControl>
                            <Input
                              type='password'
                              placeholder={t('Enter Creem API key')}
                              autoComplete='new-password'
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t('Creem API key (leave blank unless updating)')}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />

                    <FormField
                      control={form.control}
                      name='CreemWebhookSecret'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Webhook Secret')}</FormLabel>
                          <FormControl>
                            <Input
                              type='password'
                              placeholder={t('Enter webhook secret')}
                              autoComplete='new-password'
                              {...field}
                              onChange={(event) =>
                                field.onChange(event.target.value)
                              }
                            />
                          </FormControl>
                          <FormDescription>
                            {t(
                              'Webhook signing secret (leave blank unless updating)'
                            )}
                          </FormDescription>
                          <FormMessage />
                        </FormItem>
                      )}
                    />
                  </div>

                  <FormField
                    control={form.control}
                    name='CreemTestMode'
                    render={({ field }) => (
                      <FormItem className='flex flex-row items-center justify-between rounded-lg border p-4'>
                        <div className='space-y-0.5'>
                          <FormLabel className='text-base'>
                            {t('Test Mode')}
                          </FormLabel>
                          <FormDescription>
                            {t('Enable test mode for Creem payments')}
                          </FormDescription>
                        </div>
                        <FormControl>
                          <Switch
                            checked={field.value}
                            onCheckedChange={field.onChange}
                          />
                        </FormControl>
                      </FormItem>
                    )}
                  />

                  <FormField
                    control={form.control}
                    name='CreemProducts'
                    render={({ field }) => (
                      <FormItem>
                        <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                          <FormLabel>{t('Products')}</FormLabel>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() =>
                              setCreemProductsVisualMode(
                                !creemProductsVisualMode
                              )
                            }
                            className='w-full sm:w-auto'
                          >
                            {creemProductsVisualMode ? (
                              <>
                                <Code2 className='mr-2 h-3 w-3' />
                                {t('JSON Editor')}
                              </>
                            ) : (
                              <>
                                <Eye className='mr-2 h-3 w-3' />
                                {t('Visual Editor')}
                              </>
                            )}
                          </Button>
                        </div>
                        <FormControl>
                          {creemProductsVisualMode ? (
                            <CreemProductsVisualEditor
                              value={field.value}
                              onChange={field.onChange}
                            />
                          ) : (
                            <JsonCodeEditor
                              value={field.value}
                              onChange={field.onChange}
                              name={field.name}
                              onBlur={field.onBlur}
                              textareaRef={field.ref}
                              placeholder='[{"name":"Basic","productId":"prod_xxx","price":10,"quota":500000,"currency":"USD"}]'
                              heightClassName='h-40 min-h-40 max-h-40'
                              aria-invalid={Boolean(
                                form.formState.errors.CreemProducts
                              )}
                            />
                          )}
                        </FormControl>
                        <FormDescription>
                          {t('Configure Creem products. Provide a JSON array.')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />

                  <Button
                    type='button'
                    onClick={(e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      saveCreemSettings()
                    }}
                    disabled={updateOptions.isPending}
                  >
                    {updateOptions.isPending
                      ? t('Saving...')
                      : t('Save Creem settings')}
                  </Button>
                </div>
              </form>
            </Form>

            <div className={activeProvider === 'waffo' ? 'block' : 'hidden'}>
              <WaffoSettingsSection defaultValues={waffoDefaultValues} />
            </div>

            <div
              className={
                activeProvider === 'waffo-pancake' ? 'block' : 'hidden'
              }
            >
              <WaffoPancakeSettingsSection
                defaultValues={waffoPancakeDefaultValues}
              />
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </SettingsSection>
  )
}
