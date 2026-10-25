import { useState, useEffect, useCallback, useMemo } from 'react'
import {
  Mail,
  Globe,
  MessageCircle,
  Send,
  Link2,
  Unlink,
  Loader2,
  Eye,
  EyeOff,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { IconDiscord, IconGithub } from '@/assets/brand-icons'
import { api } from '@/lib/api'
import { indexCustomOAuthBindings, type CustomOAuthBinding } from '@/lib/oauth'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { StatusBadge } from '@/components/status-badge'
import {
  SecureVerificationDialog,
  useSecureVerification,
  type SecurityProof,
} from '@/features/auth/secure-verification'
import {
  getUser,
  getUserOAuthBindings,
  adminClearUserBinding,
  adminUnbindCustomOAuth,
} from '../../api'
import {
  BUILTIN_BINDING_TYPE_BY_FIELD,
  type BuiltinBindingField,
  type BuiltinBindingType,
} from '../../lib/builtin-binding-types'
import type { User } from '../../types'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  userId: number | null
  onUnbindSuccess?: () => void
}

interface BindingItem {
  key: string
  label: string
  icon: React.ReactNode
  value: string
  type: 'builtin' | 'custom'
  bindingType?: BuiltinBindingType
  providerId?: number
  isBound: boolean
  isEnabled: boolean
}

interface StatusInfo {
  github_oauth?: boolean
  discord_oauth?: boolean
  oidc_enabled?: boolean
  wechat_login?: boolean
  telegram_oauth?: boolean
  linuxdo_oauth?: boolean
  custom_oauth_providers?: Array<{
    id: number
    name: string
    icon?: string
  }>
}

const BUILTIN_BINDINGS: ReadonlyArray<{
  field: BuiltinBindingField
  label: string
  icon: React.ReactNode
  statusKey: keyof StatusInfo | null
}> = [
  {
    field: 'email',
    label: 'Email',
    icon: <Mail className='h-4 w-4' />,
    statusKey: null,
  },
  {
    field: 'github_id',
    label: 'GitHub',
    icon: <IconGithub className='h-4 w-4' />,
    statusKey: 'github_oauth',
  },
  {
    field: 'discord_id',
    label: 'Discord',
    icon: <IconDiscord className='h-4 w-4' />,
    statusKey: 'discord_oauth',
  },
  {
    field: 'wechat_id',
    label: 'WeChat',
    icon: <MessageCircle className='h-4 w-4' />,
    statusKey: 'wechat_login',
  },
  {
    field: 'oidc_id',
    label: 'OIDC',
    icon: <Globe className='h-4 w-4' />,
    statusKey: 'oidc_enabled',
  },
  {
    field: 'telegram_id',
    label: 'Telegram',
    icon: <Send className='h-4 w-4' />,
    statusKey: 'telegram_oauth',
  },
  {
    field: 'linux_do_id',
    label: 'LinuxDO',
    icon: <Globe className='h-4 w-4' />,
    statusKey: 'linuxdo_oauth',
  },
]

function CustomProviderIcon(props: { iconUrl?: string }) {
  if (!props.iconUrl) return <Link2 className='h-4 w-4' />
  return (
    <img
      src={props.iconUrl}
      alt=''
      className='h-4 w-4 rounded-sm object-contain'
      onError={(e) => {
        e.currentTarget.style.display = 'none'
      }}
    />
  )
}

export function UserBindingDialog(props: Props) {
  const { t } = useTranslation()
  const [user, setUser] = useState<User | null>(null)
  const [oauthBindings, setOauthBindings] = useState<CustomOAuthBinding[]>([])
  const [statusInfo, setStatusInfo] = useState<StatusInfo>({})
  const [loading, setLoading] = useState(false)
  const [showBoundOnly, setShowBoundOnly] = useState(true)
  const [unbindTarget, setUnbindTarget] = useState<BindingItem | null>(null)
  const [unbinding, setUnbinding] = useState(false)

  const fetchData = useCallback(async () => {
    if (!props.userId) return
    setLoading(true)
    try {
      const [userRes, oauthRes, statusRes] = await Promise.all([
        getUser(props.userId),
        getUserOAuthBindings(props.userId).catch(() => ({
          success: false,
          data: [],
        })),
        api
          .get('/api/status')
          .then((r) => r.data)
          .catch(() => ({
            success: false,
            data: {},
          })),
      ])
      if (userRes.success && userRes.data) {
        setUser(userRes.data)
      }
      if (oauthRes.success && oauthRes.data) {
        setOauthBindings(oauthRes.data)
      }
      if (statusRes.success && statusRes.data) {
        setStatusInfo(statusRes.data as StatusInfo)
      }
    } catch {
      toast.error(t('Failed to load'))
    } finally {
      setLoading(false)
    }
  }, [props.userId, t])

  useEffect(() => {
    if (props.open && props.userId) {
      setShowBoundOnly(true)
      fetchData()
    } else {
      setUser(null)
      setOauthBindings([])
      setStatusInfo({})
    }
  }, [props.open, props.userId, fetchData])

  const allBindings = useMemo<BindingItem[]>(() => {
    const items: BindingItem[] = []

    for (const field of BUILTIN_BINDINGS) {
      const value = user
        ? String((user as Record<string, unknown>)[field.field] || '')
        : ''
      const isBound = !!value
      const isEnabled =
        field.statusKey == null ? true : Boolean(statusInfo[field.statusKey])

      items.push({
        key: field.field,
        label: field.label,
        icon: field.icon,
        value: isBound ? value : '',
        type: 'builtin',
        bindingType: BUILTIN_BINDING_TYPE_BY_FIELD[field.field],
        isBound,
        isEnabled,
      })
    }

    const oauthBindingMap = indexCustomOAuthBindings(oauthBindings)

    const customProviders = statusInfo.custom_oauth_providers || []
    const seenProviderIds = new Set<number>()

    for (const provider of customProviders) {
      seenProviderIds.add(provider.id)
      const binding = oauthBindingMap.get(provider.id)
      items.push({
        key: `oauth_${provider.id}`,
        label: provider.name || String(provider.id),
        icon: <CustomProviderIcon iconUrl={provider.icon} />,
        value: binding?.provider_user_id || '',
        type: 'custom',
        providerId: provider.id,
        isBound: !!binding,
        isEnabled: true,
      })
    }

    for (const binding of oauthBindings) {
      if (!seenProviderIds.has(binding.provider_id)) {
        items.push({
          key: `oauth_${binding.provider_id}`,
          label: binding.provider_name || String(binding.provider_id),
          icon: <Link2 className='h-4 w-4' />,
          value: binding.provider_user_id || '-',
          type: 'custom',
          providerId: binding.provider_id,
          isBound: true,
          isEnabled: false,
        })
      }
    }

    return items
  }, [user, oauthBindings, statusInfo])

  const displayedBindings = showBoundOnly
    ? allBindings.filter((b) => b.isBound)
    : allBindings

  const boundCount = allBindings.filter((b) => b.isBound).length

  const verification = useSecureVerification()

  const handleUnbind = async () => {
    if (!unbindTarget || !props.userId) return
    const target = unbindTarget
    const userId = props.userId
    const builtin = target.type === 'builtin' && target.bindingType
    if (!builtin && !target.providerId) return

    const submit = async (proof?: SecurityProof) => {
      const res =
        builtin && target.bindingType
          ? await adminClearUserBinding(
              userId,
              target.bindingType,
              proof?.proof_token
            )
          : await adminUnbindCustomOAuth(
              userId,
              target.providerId as number,
              proof?.proof_token
            )
      if (res?.success) {
        toast.success(t('Unbound {{provider}}', { provider: target.label }))
        await fetchData()
        props.onUnbindSuccess?.()
      } else {
        toast.error(res?.message || t('Unbind failed'))
      }
    }

    setUnbinding(true)
    try {
      await verification.startVerification(submit, {
        operation: {
          scope: 'admin.user.binding.clear',
          context: builtin
            ? { user_id: userId, binding_type: target.bindingType }
            : { user_id: userId, provider_id: target.providerId },
        },
        description: t('Confirm your identity before removing this binding.'),
      })
    } catch {
      toast.error(t('Unbind failed'))
    } finally {
      setUnbinding(false)
      setUnbindTarget(null)
    }
  }

  return (
    <>
      <Dialog open={props.open} onOpenChange={props.onOpenChange}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle className='flex items-center gap-2'>
              <Link2 className='h-5 w-5' />
              {t('Account Binding Management')}
            </DialogTitle>
            <DialogDescription className='sr-only'>
              {t('Manage account bindings for this user')}
            </DialogDescription>
          </DialogHeader>

          {loading ? (
            <div className='flex items-center justify-center py-8'>
              <Loader2 className='text-muted-foreground h-6 w-6 animate-spin' />
            </div>
          ) : (
            <div className='space-y-3'>
              <div className='flex items-center justify-between'>
                {user && (
                  <p className='text-muted-foreground text-sm'>
                    {user.username} (ID: {user.id})
                  </p>
                )}
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant='ghost'
                        size='sm'
                        className='h-7 gap-1.5 px-2 text-xs'
                        onClick={() => setShowBoundOnly((v) => !v)}
                      >
                        {showBoundOnly ? (
                          <Eye className='h-3.5 w-3.5' />
                        ) : (
                          <EyeOff className='h-3.5 w-3.5' />
                        )}
                        {showBoundOnly ? t('Show All') : t('Bound Only')}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {showBoundOnly
                        ? t('Show all providers including unbound')
                        : t('Show only bound providers')}
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </div>

              <Separator />

              <ScrollArea className='max-h-[50vh]'>
                {displayedBindings.length === 0 ? (
                  <p className='text-muted-foreground py-4 text-center text-sm'>
                    {showBoundOnly
                      ? t('This user has no bindings')
                      : t('No providers available')}
                  </p>
                ) : (
                  <div className='grid grid-cols-1 gap-2 pr-3 lg:grid-cols-2'>
                    {displayedBindings.map((binding) => (
                      <div
                        key={binding.key}
                        className={`flex items-center justify-between rounded-md border px-3 py-2.5 ${
                          !binding.isBound ? 'opacity-50' : ''
                        }`}
                      >
                        <div className='flex min-w-0 items-center gap-2.5'>
                          <div className='text-muted-foreground shrink-0'>
                            {binding.icon}
                          </div>
                          <div className='min-w-0'>
                            <div className='flex items-center gap-1.5'>
                              <span className='text-sm font-medium'>
                                {binding.label}
                              </span>
                              {!binding.isEnabled && (
                                <StatusBadge
                                  variant='neutral'
                                  label={t('Disabled')}
                                  copyable={false}
                                  size='sm'
                                />
                              )}
                            </div>
                            <p className='text-muted-foreground max-w-[140px] truncate text-xs'>
                              {binding.isBound ? binding.value : t('Not bound')}
                            </p>
                          </div>
                        </div>
                        {binding.isBound && (
                          <Button
                            variant='ghost'
                            size='sm'
                            className='text-destructive hover:text-destructive h-7 w-7 shrink-0 p-0'
                            onClick={() => setUnbindTarget(binding)}
                          >
                            <Unlink className='h-3.5 w-3.5' />
                          </Button>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </ScrollArea>

              <p className='text-muted-foreground text-xs'>
                {t('Bound')}: {boundCount} / {allBindings.length}
              </p>
            </div>
          )}
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={!!unbindTarget}
        onOpenChange={(open) => !open && setUnbindTarget(null)}
        title={t('Confirm Unbind')}
        desc={t(
          'Are you sure you want to unbind {{provider}} for this user? The user will no longer be able to log in via this method.',
          {
            provider: unbindTarget?.label || '',
          }
        )}
        confirmText={t('Confirm Unbind')}
        destructive
        handleConfirm={handleUnbind}
        isLoading={unbinding}
      />

      <SecureVerificationDialog {...verification.dialogProps} />
    </>
  )
}
