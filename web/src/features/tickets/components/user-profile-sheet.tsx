import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { RefreshCw, User } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { api } from '@/lib/api'
import { formatQuotaWithCurrency } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { formatTicketTimestamp } from '../lib/ticket-utils'

type RecentLog = {
  id: number
  created_at?: number
  model_name?: string
  token_name?: string
  quota?: number
  prompt_tokens?: number
  completion_tokens?: number
}

type ModelUsage = {
  model_name?: string
  count?: number
  quota?: number
  token_used?: number
}

type UserProfile = {
  user_id: number
  username: string
  display_name?: string
  email?: string
  role: number
  status: number
  group?: string
  created_time?: number
  quota?: number
  used_quota?: number
  request_count?: number
  pending_refund_quota?: number
  recent_logs?: RecentLog[]
  model_usage?: ModelUsage[]
}

function roleLabel(role: number, t: (k: string) => string): string {
  if (role >= 100) return t('Super admin')
  if (role >= 10) return t('Admin')
  if (role >= 5) return t('Support')
  return t('User')
}

function StatusBadge({ status }: { status: number }) {
  const { t } = useTranslation()
  if (status === 1) {
    return (
      <Badge
        variant='outline'
        className='border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400'
      >
        {t('Enabled')}
      </Badge>
    )
  }
  return (
    <Badge
      variant='outline'
      className='border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
    >
      {t('Disabled')}
    </Badge>
  )
}

type Props = {
  ticketId: number
  username?: string
  userId?: number
}

export function TicketUserProfileSheet({ ticketId, username, userId }: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [profile, setProfile] = useState<UserProfile | null>(null)
  const attempted = useRef(false)

  const load = async () => {
    if (!ticketId) return
    setLoading(true)
    attempted.current = true
    try {
      const res = await api.get(`/api/ticket/admin/${ticketId}/user-profile`)
      if (res.data?.success) {
        setProfile(res.data.data || null)
      } else {
        toast.error(res.data?.message || t('Failed to load user profile'))
      }
    } catch (e: unknown) {
      const status = (e as { response?: { status?: number } })?.response?.status
      toast.error(
        status === 404
          ? t('This feature requires a backend update')
          : t('Request failed')
      )
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (open && !attempted.current && !loading) {
      load()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button type='button' variant='outline'>
          <User className='mr-1.5 h-3.5 w-3.5' />
          {t('User profile')}
        </Button>
      </SheetTrigger>
      <SheetContent className='flex w-full flex-col gap-0 p-0 sm:max-w-2xl'>
        <SheetHeader className='border-b px-4 py-3 sm:px-6 sm:py-4'>
          <div className='flex items-center justify-between gap-2'>
            <div>
              <SheetTitle>{t('User profile')}</SheetTitle>
              <SheetDescription>
                {username || ''}
                {userId ? ` · UID ${userId}` : ''}
              </SheetDescription>
            </div>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={load}
              disabled={loading}
            >
              <RefreshCw
                className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')}
              />
              {t('Refresh')}
            </Button>
          </div>
        </SheetHeader>

        <div className='flex-1 space-y-5 overflow-y-auto p-4 sm:p-5'>
          {loading && !profile ? (
            <Skeleton className='h-40 w-full' />
          ) : !profile ? (
            <div className='text-muted-foreground py-12 text-center text-sm'>
              {t('No user profile data')}
            </div>
          ) : (
            <>
              {/* Basic info */}
              <section className='bg-card rounded-xl border p-4'>
                <dl className='grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2'>
                  <Row label={t('User')}>
                    {profile.username || '—'} (UID: {profile.user_id || '—'})
                  </Row>
                  <Row label={t('Display name')}>
                    {profile.display_name || '—'}
                  </Row>
                  <Row label={t('Email')}>{profile.email || '—'}</Row>
                  <Row label={t('Role')}>{roleLabel(profile.role, t)}</Row>
                  <Row label={t('Status')}>
                    <StatusBadge status={profile.status} />
                  </Row>
                  <Row label={t('Group')}>{profile.group || 'default'}</Row>
                  <Row label={t('Registered at')}>
                    {profile.created_time
                      ? formatTicketTimestamp(profile.created_time)
                      : '—'}
                  </Row>
                  <Row label={t('Current balance')}>
                    <span className='font-semibold'>
                      {formatQuotaWithCurrency(profile.quota || 0)}
                    </span>
                  </Row>
                  <Row label={t('Total used')}>
                    {formatQuotaWithCurrency(profile.used_quota || 0)}
                  </Row>
                  <Row label={t('Request count')}>
                    {Number(profile.request_count || 0)}
                  </Row>
                  {profile.pending_refund_quota != null &&
                    profile.pending_refund_quota > 0 && (
                      <Row label={t('Pending refund')} className='sm:col-span-2'>
                        <span className='inline-flex flex-wrap items-center gap-2'>
                          {formatQuotaWithCurrency(profile.pending_refund_quota)}
                          <Badge
                            variant='outline'
                            className='border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400 font-normal'
                          >
                            {t('Already deducted, awaiting review')}
                          </Badge>
                        </span>
                      </Row>
                    )}
                </dl>
              </section>

              {/* Recent API calls */}
              <section>
                <h4 className='mb-2 text-sm font-semibold'>
                  {t('Recent API calls')}
                </h4>
                <div className='overflow-x-auto rounded-md border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className='w-40'>{t('Time')}</TableHead>
                        <TableHead>{t('Model')}</TableHead>
                        <TableHead>{t('Token')}</TableHead>
                        <TableHead className='w-24 text-right'>
                          {t('Cost')}
                        </TableHead>
                        <TableHead className='w-20 text-right'>
                          {t('In')}
                        </TableHead>
                        <TableHead className='w-20 text-right'>
                          {t('Out')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {(profile.recent_logs || []).length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={6}
                            className='text-muted-foreground py-6 text-center text-sm'
                          >
                            {t('No recent API calls')}
                          </TableCell>
                        </TableRow>
                      ) : (
                        (profile.recent_logs || []).map((log) => (
                          <TableRow key={log.id}>
                            <TableCell className='text-muted-foreground text-xs'>
                              {formatTicketTimestamp(log.created_at)}
                            </TableCell>
                            <TableCell className='max-w-[160px] truncate font-mono text-xs'>
                              {log.model_name || '—'}
                            </TableCell>
                            <TableCell className='max-w-[120px] truncate text-xs'>
                              {log.token_name || '—'}
                            </TableCell>
                            <TableCell className='text-right'>
                              {formatQuotaWithCurrency(Number(log.quota || 0))}
                            </TableCell>
                            <TableCell className='text-right font-mono text-xs'>
                              {Number(log.prompt_tokens || 0)}
                            </TableCell>
                            <TableCell className='text-right font-mono text-xs'>
                              {Number(log.completion_tokens || 0)}
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </div>
              </section>

              {/* Top models */}
              <section>
                <h4 className='mb-2 text-sm font-semibold'>
                  {t('Top models (last 30 days)')}
                </h4>
                <div className='overflow-x-auto rounded-md border'>
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('Model')}</TableHead>
                        <TableHead className='w-24 text-right'>
                          {t('Calls')}
                        </TableHead>
                        <TableHead className='w-32 text-right'>
                          {t('Cost')}
                        </TableHead>
                        <TableHead className='w-28 text-right'>
                          {t('Tokens')}
                        </TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {(profile.model_usage || []).length === 0 ? (
                        <TableRow>
                          <TableCell
                            colSpan={4}
                            className='text-muted-foreground py-6 text-center text-sm'
                          >
                            {t('No model usage data')}
                          </TableCell>
                        </TableRow>
                      ) : (
                        (profile.model_usage || []).map((m) => (
                          <TableRow key={m.model_name}>
                            <TableCell className='max-w-[200px] truncate font-mono text-xs'>
                              {m.model_name || '—'}
                            </TableCell>
                            <TableCell className='text-right font-mono text-xs'>
                              {Number(m.count || 0)}
                            </TableCell>
                            <TableCell className='text-right'>
                              {formatQuotaWithCurrency(Number(m.quota || 0))}
                            </TableCell>
                            <TableCell className='text-right font-mono text-xs'>
                              {Number(m.token_used || 0)}
                            </TableCell>
                          </TableRow>
                        ))
                      )}
                    </TableBody>
                  </Table>
                </div>
              </section>
            </>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function Row({
  label,
  children,
  className,
}: {
  label: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={className}>
      <dt className='text-muted-foreground text-xs'>{label}</dt>
      <dd className='mt-0.5'>{children}</dd>
    </div>
  )
}
