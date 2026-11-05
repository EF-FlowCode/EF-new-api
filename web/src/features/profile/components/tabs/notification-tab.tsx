import { useState, useEffect, useCallback } from 'react'
import { Bell, Loader2, Mail, Server, Webhook } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { useAuthStore } from '@/stores/auth-store'
import { getCurrencyLabel } from '@/lib/currency'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'
import { ROLE } from '@/lib/roles'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Switch } from '@/components/ui/switch'
import { PasswordInput } from '@/components/password-input'
import { updateUserSettings } from '../../api'
import {
  DEFAULT_QUOTA_WARNING_THRESHOLD,
  DEFAULT_TOKEN_QUOTA_WARNING_PERCENT,
  NOTIFICATION_METHODS,
} from '../../constants'
import { parseUserSettings } from '../../lib'
import type { UserProfile, UserSettings, NotifyType } from '../../types'

const NOTIFICATION_ICONS: Record<string, typeof Mail> = {
  email: Mail,
  webhook: Webhook,
  bark: Bell,
  gotify: Server,
}

// ============================================================================
// Settings Tab Component
// ============================================================================

interface NotificationTabProps {
  profile: UserProfile | null
  onUpdate: () => void
}

export function NotificationTab({ profile, onUpdate }: NotificationTabProps) {
  const { t } = useTranslation()
  const isAdmin = (profile?.role ?? 0) >= ROLE.ADMIN
  const [loading, setLoading] = useState(false)
  const [settings, setSettings] = useState<UserSettings>({
    notify_type: 'email',
    quota_warning_threshold: DEFAULT_QUOTA_WARNING_THRESHOLD,
    notification_email: '',
    webhook_url: '',
    webhook_secret: '',
    bark_url: '',
    gotify_url: '',
    gotify_token: '',
    gotify_priority: 5,
    accept_unset_model_ratio_model: false,
    record_ip_log: false,
    upstream_model_update_notify_enabled: false,
    token_quota_warning_disabled: false,
    token_quota_warning_percent: DEFAULT_TOKEN_QUOTA_WARNING_PERCENT,
    token_quota_warning_amount: 0,
  })

  // Update form field helper
  const updateField = useCallback(
    <K extends keyof UserSettings>(field: K, value: UserSettings[K]) => {
      setSettings((prev) => ({ ...prev, [field]: value }))
    },
    []
  )

  useEffect(() => {
    if (profile?.setting) {
      const parsed = parseUserSettings(profile.setting)
      setSettings({
        notify_type: parsed.notify_type || 'email',
        quota_warning_threshold:
          parsed.quota_warning_threshold ?? DEFAULT_QUOTA_WARNING_THRESHOLD,
        notification_email: parsed.notification_email ?? '',
        webhook_url: parsed.webhook_url ?? '',
        webhook_secret: parsed.webhook_secret ?? '',
        bark_url: parsed.bark_url ?? '',
        gotify_url: parsed.gotify_url ?? '',
        gotify_token: parsed.gotify_token ?? '',
        gotify_priority: parsed.gotify_priority ?? 5,
        accept_unset_model_ratio_model:
          parsed.accept_unset_model_ratio_model || false,
        record_ip_log: parsed.record_ip_log || false,
        upstream_model_update_notify_enabled:
          parsed.upstream_model_update_notify_enabled || false,
        token_quota_warning_disabled:
          parsed.token_quota_warning_disabled || false,
        token_quota_warning_percent:
          parsed.token_quota_warning_percent ??
          DEFAULT_TOKEN_QUOTA_WARNING_PERCENT,
        token_quota_warning_amount: parsed.token_quota_warning_amount ?? 0,
      })
    }
  }, [profile])

  const handleSave = async () => {
    try {
      setLoading(true)
      const response = await updateUserSettings(settings)

      if (response.success) {
        toast.success(t('Settings updated successfully'))
        // The API key list reads the warning settings from the auth store.
        const { user, setUser } = useAuthStore.getState().auth
        if (user) {
          const current =
            typeof user.setting === 'string'
              ? parseUserSettings(user.setting)
              : (user.setting ?? {})
          setUser({ ...user, setting: { ...current, ...settings } })
        }
        onUpdate()
      } else {
        toast.error(response.message || t('Failed to update settings'))
      }
    } catch (_error) {
      toast.error(t('Failed to update settings'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className='space-y-4 sm:space-y-6'>
      {/* Notification Type */}
      <div className='space-y-2.5'>
        <Label>{t('Notification Method')}</Label>
        <RadioGroup
          value={settings.notify_type}
          onValueChange={(value) =>
            updateField('notify_type', value as NotifyType)
          }
          className='grid grid-cols-4 gap-1.5 sm:gap-3'
        >
          {NOTIFICATION_METHODS.map((method) => {
            const Icon = NOTIFICATION_ICONS[method.value]
            const isSelected = settings.notify_type === method.value
            return (
              <Label
                key={method.value}
                htmlFor={method.value}
                className={`flex min-h-16 cursor-pointer flex-col items-center justify-center gap-1.5 rounded-lg border p-2 text-center transition-colors sm:min-h-20 sm:gap-2 sm:border-2 sm:p-3 ${
                  isSelected
                    ? 'border-primary bg-primary/5 text-primary'
                    : 'border-muted hover:border-muted-foreground/25 hover:bg-muted/50'
                }`}
              >
                <RadioGroupItem
                  value={method.value}
                  id={method.value}
                  className='sr-only'
                />
                <Icon className='h-4 w-4 sm:h-5 sm:w-5' />
                <span className='max-w-full truncate text-xs font-medium sm:text-sm'>
                  {t(method.label)}
                </span>
              </Label>
            )
          })}
        </RadioGroup>
      </div>

      {/* Warning Threshold */}
      <div className='space-y-1.5'>
        <Label htmlFor='threshold'>
          {t('Balance Warning Threshold ({{currency}})', {
            currency: getCurrencyLabel(),
          })}
        </Label>
        <Input
          id='threshold'
          type='number'
          min={0}
          step={0.01}
          className='h-9'
          value={quotaUnitsToDollars(settings.quota_warning_threshold ?? 0)}
          onChange={(e) =>
            updateField(
              'quota_warning_threshold',
              Math.max(
                0,
                Math.round(parseQuotaFromDollars(Number(e.target.value) || 0))
              )
            )
          }
          placeholder={t('Enter threshold')}
        />
        <p className='text-muted-foreground text-xs'>
          {t('Notify once when your balance falls below this amount')}
        </p>
      </div>

      {/* eflow: API key quota warning (all limited keys) */}
      <div className='space-y-3 rounded-lg border p-3 sm:p-4'>
        <div className='flex items-start justify-between gap-3 sm:items-center'>
          <div className='space-y-0.5'>
            <Label htmlFor='tokenQuotaWarning'>
              {t('API Key Quota Warning')}
            </Label>
            <p className='text-muted-foreground text-xs sm:text-sm'>
              {t(
                'Notify once when a limited API key falls below either line; topping the key back up re-arms the warning.'
              )}
            </p>
          </div>
          <Switch
            id='tokenQuotaWarning'
            className='shrink-0'
            checked={!settings.token_quota_warning_disabled}
            onCheckedChange={(checked) =>
              updateField('token_quota_warning_disabled', !checked)
            }
          />
        </div>
        {!settings.token_quota_warning_disabled && (
          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label htmlFor='tokenQuotaWarningPercent'>
                {t('Below percentage of key total (%)')}
              </Label>
              <Input
                id='tokenQuotaWarningPercent'
                type='number'
                min={0}
                max={100}
                step={0.1}
                className='h-9'
                value={settings.token_quota_warning_percent}
                onChange={(e) =>
                  updateField(
                    'token_quota_warning_percent',
                    Number(e.target.value)
                  )
                }
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='tokenQuotaWarningAmount'>
                {t('Below amount ({{currency}})', {
                  currency: getCurrencyLabel(),
                })}
              </Label>
              <Input
                id='tokenQuotaWarningAmount'
                type='number'
                min={0}
                step={0.01}
                className='h-9'
                value={quotaUnitsToDollars(
                  settings.token_quota_warning_amount ?? 0
                )}
                onChange={(e) =>
                  updateField(
                    'token_quota_warning_amount',
                    Math.max(
                      0,
                      Math.round(
                        parseQuotaFromDollars(Number(e.target.value) || 0)
                      )
                    )
                  )
                }
              />
            </div>
            <p className='text-muted-foreground text-xs sm:col-span-2'>
              {t('Set either value to 0 to turn that condition off.')}
            </p>
          </div>
        )}
      </div>

      {/* Email Settings */}
      {settings.notify_type === 'email' && (
        <div className='space-y-1.5'>
          <Label htmlFor='notifyEmail'>{t('Notification Email')}</Label>
          <Input
            id='notifyEmail'
            type='email'
            className='h-9'
            value={settings.notification_email}
            onChange={(e) => updateField('notification_email', e.target.value)}
            placeholder={t('Leave empty to use account email')}
          />
        </div>
      )}

      {/* Webhook Settings */}
      {settings.notify_type === 'webhook' && (
        <>
          <div className='space-y-1.5'>
            <Label htmlFor='webhookUrl'>{t('Webhook URL')}</Label>
            <Input
              id='webhookUrl'
              type='url'
              className='h-9'
              value={settings.webhook_url}
              onChange={(e) => updateField('webhook_url', e.target.value)}
              placeholder={t('https://example.com/webhook')}
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='webhookSecret'>{t('Webhook Secret')}</Label>
            <PasswordInput
              id='webhookSecret'
              value={settings.webhook_secret}
              onChange={(e) => updateField('webhook_secret', e.target.value)}
              placeholder={t('Enter secret key')}
            />
          </div>
        </>
      )}

      {/* Bark Settings */}
      {settings.notify_type === 'bark' && (
        <div className='space-y-1.5'>
          <Label htmlFor='barkUrl'>{t('Bark Push URL')}</Label>
          <Input
            id='barkUrl'
            type='url'
            className='h-9'
            value={settings.bark_url}
            onChange={(e) => updateField('bark_url', e.target.value)}
            placeholder={t('https://api.day.app/yourkey/{{title}}/{{content}}')}
          />
          <p className='text-muted-foreground text-xs'>
            {t('Template variables:')} {'{{title}}'}, {'{{content}}'}
          </p>
        </div>
      )}

      {/* Gotify Settings */}
      {settings.notify_type === 'gotify' && (
        <>
          <div className='space-y-1.5'>
            <Label htmlFor='gotifyUrl'>{t('Gotify Server URL')}</Label>
            <Input
              id='gotifyUrl'
              type='url'
              className='h-9'
              value={settings.gotify_url}
              onChange={(e) => updateField('gotify_url', e.target.value)}
              placeholder={t('https://gotify.example.com')}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Enter the full URL of your Gotify server')}
            </p>
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='gotifyToken'>{t('Gotify Application Token')}</Label>
            <PasswordInput
              id='gotifyToken'
              value={settings.gotify_token}
              onChange={(e) => updateField('gotify_token', e.target.value)}
              placeholder={t('Enter application token')}
            />
            <p className='text-muted-foreground text-xs'>
              {t('Token obtained from your Gotify application')}
            </p>
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='gotifyPriority'>{t('Message Priority')}</Label>
            <Input
              id='gotifyPriority'
              type='number'
              className='h-9'
              min='0'
              max='10'
              value={settings.gotify_priority}
              onChange={(e) =>
                updateField('gotify_priority', Number(e.target.value))
              }
              placeholder='5'
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Priority level from 0 (lowest) to 10 (highest), default is 5'
              )}
            </p>
          </div>
          <div className='bg-muted/50 rounded-lg border p-3 sm:p-4'>
            <h5 className='mb-1.5 text-sm font-medium sm:mb-2'>
              {t('Setup Instructions')}
            </h5>
            <ol className='text-muted-foreground space-y-1 text-xs'>
              <li>{t('1. Create an application in your Gotify server')}</li>
              <li>{t('2. Copy the application token')}</li>
              <li>{t('3. Enter your Gotify server URL and token above')}</li>
            </ol>
            <p className='text-muted-foreground mt-3 text-xs'>
              {t('Learn more:')}{' '}
              <a
                href='https://gotify.net/'
                target='_blank'
                rel='noopener noreferrer'
                className='text-primary hover:underline'
              >
                {t('Gotify Documentation')}
              </a>
            </p>
          </div>
        </>
      )}

      {/* Divider */}
      <div className='border-t' />

      {/* Preferences Section */}
      <div className='space-y-3'>
        <div>
          <h4 className='text-sm font-medium'>{t('Preferences')}</h4>
          <p className='text-muted-foreground mt-1 text-xs'>
            {t('Configure your account behavior preferences')}
          </p>
        </div>

        {/* Receive Upstream Model Update Notifications (admin only) */}
        {isAdmin && (
          <div className='flex items-start justify-between gap-3 rounded-lg border p-3 sm:items-center sm:p-4'>
            <div className='space-y-0.5'>
              <Label htmlFor='upstreamModelUpdateNotify'>
                {t('Receive Upstream Model Update Notifications')}
              </Label>
              <p className='text-muted-foreground line-clamp-3 text-xs sm:line-clamp-none sm:text-sm'>
                {t(
                  'Only available for admins. When enabled, you will receive a summary notification via your selected method when the scheduled model check detects upstream model changes or check failures.'
                )}
              </p>
            </div>
            <Switch
              id='upstreamModelUpdateNotify'
              className='shrink-0'
              checked={settings.upstream_model_update_notify_enabled}
              onCheckedChange={(checked) =>
                updateField('upstream_model_update_notify_enabled', checked)
              }
            />
          </div>
        )}

        {/* Accept Unset Model Price */}
        <div className='flex items-start justify-between gap-3 rounded-lg border p-3 sm:items-center sm:p-4'>
          <div className='space-y-0.5'>
            <Label htmlFor='acceptUnsetPrice'>
              {t('Accept Unpriced Models')}
            </Label>
            <p className='text-muted-foreground text-xs sm:text-sm'>
              {t('Allow using models without price configuration')}
            </p>
          </div>
          <Switch
            id='acceptUnsetPrice'
            className='shrink-0'
            checked={settings.accept_unset_model_ratio_model}
            onCheckedChange={(checked) =>
              updateField('accept_unset_model_ratio_model', checked)
            }
          />
        </div>

        {/* Record IP Log */}
        <div className='flex items-start justify-between gap-3 rounded-lg border p-3 sm:items-center sm:p-4'>
          <div className='space-y-0.5'>
            <Label htmlFor='recordIp'>{t('Record IP Address')}</Label>
            <p className='text-muted-foreground text-xs sm:text-sm'>
              {t('Log IP address for usage and error logs')}
            </p>
          </div>
          <Switch
            id='recordIp'
            className='shrink-0'
            checked={settings.record_ip_log}
            onCheckedChange={(checked) => updateField('record_ip_log', checked)}
          />
        </div>
      </div>

      {/* Save Button */}
      <div className='flex justify-end'>
        <Button onClick={handleSave} disabled={loading}>
          {loading && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
          {loading ? t('Saving...') : t('Save Settings')}
        </Button>
      </div>
    </div>
  )
}
