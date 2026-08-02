import { useEffect, useState } from 'react'
import { Copy, Link2, Plus, RefreshCw, Settings2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatLocalCurrencyAmount } from '@/lib/currency'
import { useIsStaff } from '@/hooks/use-admin'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  fetchMyInvitationCodeQuota,
  fetchMyInvitationCodes,
  generateMyInvitationCode,
} from './api'
import {
  INVITATION_CODE_STATUS,
  type InvitationCode,
  type MyInvitationCodeQuota,
} from './types'

const formatDate = (sec: number) => {
  if (!sec) return ''
  const d = new Date(sec * 1000)
  return d.toLocaleString()
}

// 把"无限"配额显示成 ∞，其它正常数字。
const renderUnlimitedOr = (n: number) => (n < 0 ? '∞' : String(n))

// "近 30 天还剩 X 天释放" 的相对描述。
const formatRelativeDays = (sec: number): string | null => {
  if (!sec) return null
  const nowMs = Date.now()
  const targetMs = sec * 1000
  const diffMs = targetMs - nowMs
  if (diffMs <= 0) return null
  const days = Math.ceil(diffMs / 86400_000)
  if (days <= 1) {
    const hours = Math.max(1, Math.ceil(diffMs / 3_600_000))
    return `${hours}h`
  }
  return `${days}d`
}

export function MyInvitationCodes() {
  const { t } = useTranslation()
  // hook 内部已经 toast，无需再手动 toast.success / error
  const { copyToClipboard } = useCopyToClipboard()
  // eflow: 客服 (role>=5) / admin 解锁批量 + 自定义参数的折叠表单。
  const isStaff = useIsStaff()
  const [codes, setCodes] = useState<InvitationCode[]>([])
  const [quota, setQuota] = useState<MyInvitationCodeQuota | null>(null)
  const [loading, setLoading] = useState(true)
  const [generating, setGenerating] = useState(false)
  // staff 折叠表单状态——默认收起、空值代表"按全局策略"
  const [staffFormOpen, setStaffFormOpen] = useState(false)
  const [staffCount, setStaffCount] = useState<string>('1')
  const [staffMaxUses, setStaffMaxUses] = useState<string>('') // '' = 默认策略
  const [staffValidDays, setStaffValidDays] = useState<string>('') // '' = 默认策略；'0' = 永久

  const load = async () => {
    setLoading(true)
    try {
      const [list, q] = await Promise.all([
        fetchMyInvitationCodes(),
        fetchMyInvitationCodeQuota(),
      ])
      setCodes(list)
      setQuota(q)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const onGenerate = async () => {
    if (!quota?.can_generate) return
    setGenerating(true)
    try {
      // staff 折叠态展开且有参数时按自定义生成；否则普通单条路径。
      let payload: Parameters<typeof generateMyInvitationCode>[0] | undefined
      if (isStaff && staffFormOpen) {
        const count = Math.max(1, Math.min(100, Number(staffCount) || 1))
        const p: NonNullable<typeof payload> = { count }
        if (staffMaxUses.trim() !== '') {
          const n = Number(staffMaxUses)
          if (!Number.isFinite(n) || n < 0) {
            toast.error(t('Max uses must be ≥ 0 (0 = unlimited)'))
            setGenerating(false)
            return
          }
          p.max_uses = Math.floor(n)
        }
        if (staffValidDays.trim() !== '') {
          const d = Number(staffValidDays)
          if (!Number.isFinite(d) || d < 0) {
            toast.error(t('Valid days must be ≥ 0 (0 = permanent)'))
            setGenerating(false)
            return
          }
          p.expired_time =
            d === 0 ? 0 : Math.floor(Date.now() / 1000) + Math.floor(d) * 86400
        }
        payload = p
      }
      const result = await generateMyInvitationCode(payload)
      const arr = Array.isArray(result) ? result : [result]
      toast.success(
        arr.length === 1
          ? t('Invitation code generated')
          : t('{{n}} invitation codes generated', { n: arr.length })
      )
      // 复用列表刷新代替手工 prepend，配额信息同步刷新（monthly_used + 1 等）。
      await load()
      // 单条仍按旧行为复制到剪贴板；批量复制换行拼接整段。
      if (arr.length === 1) {
        void copyToClipboard(arr[0].code)
      } else {
        void copyToClipboard(arr.map((c) => c.code).join('\n'))
      }
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setGenerating(false)
    }
  }

  const onCopy = (text: string) => {
    void copyToClipboard(text)
  }

  const onCopyLink = (code: string) => {
    const url = `${window.location.origin}/register?invitation_code=${encodeURIComponent(code)}`
    onCopy(url)
  }

  const renderCodeStatus = (code: InvitationCode) => {
    const exhausted = code.max_uses > 0 && code.used_count >= code.max_uses
    const expired =
      code.expired_time > 0 && code.expired_time * 1000 < Date.now()
    const isActive =
      code.status === INVITATION_CODE_STATUS.ENABLED && !exhausted && !expired

    if (isActive) return <Badge variant='default'>{t('Active')}</Badge>
    if (exhausted) return <Badge variant='secondary'>{t('Exhausted')}</Badge>
    if (expired) return <Badge variant='secondary'>{t('Expired')}</Badge>
    return <Badge variant='outline'>{t('Disabled')}</Badge>
  }

  const renderQuotaCard = () => {
    if (!quota) return null
    const lifetimeLine =
      quota.limit < 0
        ? t('Unlimited')
        : t('{{used}} / {{limit}}', {
            used: quota.used,
            limit: quota.limit,
          })
    const monthlyEnabled = quota.monthly_limit > 0
    const monthlyLine = monthlyEnabled
      ? t('{{used}} / {{limit}}', {
          used: quota.monthly_used,
          limit: quota.monthly_limit,
        })
      : t('Unlimited')
    const resetIn = monthlyEnabled
      ? formatRelativeDays(quota.monthly_next_reset_at)
      : null
    const topupGateEnabled = quota.min_topup_money > 0
    // min>0 时算进度百分比，>=100% 即已解锁；min=0 时整个 block 不渲染。
    const topupPct = topupGateEnabled
      ? Math.min(100, (quota.user_topup_money / quota.min_topup_money) * 100)
      : 0
    const topupMet =
      topupGateEnabled && quota.user_topup_money >= quota.min_topup_money

    return (
      <div className='space-y-3'>
        <div className='bg-card grid gap-3 rounded-xl border p-4 sm:grid-cols-3 sm:p-5'>
          <div className='space-y-1'>
            <div className='text-muted-foreground text-xs tracking-wide uppercase'>
              {t('Lifetime quota')}
            </div>
            <div className='text-2xl font-semibold tabular-nums'>
              {lifetimeLine}
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('Remaining: {{n}}', {
                n: renderUnlimitedOr(quota.remaining),
              })}
            </div>
          </div>
          <div className='space-y-1'>
            <div className='text-muted-foreground text-xs tracking-wide uppercase'>
              {t('Rolling 30-day quota')}
            </div>
            <div className='text-2xl font-semibold tabular-nums'>
              {monthlyLine}
            </div>
            <div className='text-muted-foreground text-xs'>
              {monthlyEnabled
                ? resetIn
                  ? t('Next slot in {{when}}', { when: resetIn })
                  : t('Window not yet triggered')
                : t('Not limited')}
            </div>
          </div>
          <div className='space-y-1'>
            <div className='text-muted-foreground text-xs tracking-wide uppercase'>
              {t('Account age')}
            </div>
            <div className='text-2xl font-semibold tabular-nums'>
              {quota.account_age_days}d
            </div>
            <div className='text-muted-foreground text-xs'>
              {t('Min required: {{n}}d', { n: quota.min_account_age_days })}
            </div>
          </div>
        </div>

        {topupGateEnabled ? (
          <div className='bg-card flex flex-col gap-2 rounded-xl border p-4 sm:p-5'>
            <div className='flex items-center justify-between gap-3'>
              <div className='space-y-0.5'>
                <div className='text-muted-foreground text-xs tracking-wide uppercase'>
                  {t('Cumulative top-up')}
                </div>
                <div className='text-base font-semibold tabular-nums'>
                  {t('{{used}} / {{limit}}', {
                    used: formatLocalCurrencyAmount(quota.user_topup_money),
                    limit: formatLocalCurrencyAmount(quota.min_topup_money),
                  })}
                </div>
              </div>
              <div
                className={
                  topupMet
                    ? 'text-xs font-medium text-emerald-600'
                    : 'text-muted-foreground text-xs'
                }
              >
                {topupMet
                  ? t('Threshold reached')
                  : t('{{pct}}% to unlock', { pct: topupPct.toFixed(0) })}
              </div>
            </div>
            <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
              <div
                className={
                  topupMet ? 'h-full bg-emerald-500' : 'bg-primary h-full'
                }
                style={{ width: `${topupPct}%` }}
              />
            </div>
          </div>
        ) : null}
      </div>
    )
  }

  const generateDisabled = !quota?.can_generate || generating
  const generateReason = quota?.can_generate ? null : quota?.reason

  return (
    <div className='px-4 py-6 sm:px-8'>
      <div className='mx-auto w-full max-w-[1440px] space-y-5'>
        <div className='flex flex-col gap-1'>
          <h1 className='text-xl font-semibold tracking-tight'>
            {t('My invitation codes')}
          </h1>
          <p className='text-muted-foreground text-sm'>
            {t(
              'Generate your own invitation codes to invite new users, and view existing ones.'
            )}
          </p>
        </div>

        {loading && !quota ? (
          <Skeleton className='h-28 w-full rounded-xl' />
        ) : (
          renderQuotaCard()
        )}

        <div className='bg-card flex flex-wrap items-center gap-2 rounded-xl border p-3'>
          <Button
            type='button'
            onClick={() => void onGenerate()}
            disabled={generateDisabled}
          >
            {generating ? (
              <RefreshCw className='mr-1.5 h-3.5 w-3.5 animate-spin' />
            ) : (
              <Plus className='mr-1.5 h-3.5 w-3.5' />
            )}
            {isStaff && staffFormOpen
              ? t('Generate {{n}} code(s)', {
                  n: Math.max(1, Math.min(100, Number(staffCount) || 1)),
                })
              : t('Generate a new code')}
          </Button>
          {/* eflow: 仅 staff 见的"自定义"开关 */}
          {isStaff ? (
            <Button
              type='button'
              variant={staffFormOpen ? 'secondary' : 'ghost'}
              size='sm'
              onClick={() => setStaffFormOpen((v) => !v)}
            >
              <Settings2 className='mr-1.5 h-3.5 w-3.5' />
              {t('Customize')}
            </Button>
          ) : null}
          {generateReason ? (
            <span className='text-muted-foreground text-xs'>
              {generateReason}
            </span>
          ) : null}
          <div className='ml-auto'>
            <Button
              type='button'
              variant='ghost'
              size='sm'
              onClick={() => void load()}
              disabled={loading}
            >
              <RefreshCw className='mr-1.5 h-3.5 w-3.5' />
              {t('Refresh')}
            </Button>
          </div>
        </div>

        {/* eflow: staff 折叠表单——展开时显示三个参数；空值=按全局策略。
            count 1-100；max_uses 0=无限次；valid_days 0=永久、空=策略默认。 */}
        {isStaff && staffFormOpen ? (
          <div className='bg-card grid gap-3 rounded-xl border p-4 sm:grid-cols-3 sm:p-5'>
            <div className='space-y-1.5'>
              <Label htmlFor='staff-ic-count' className='text-xs'>
                {t('Count')}
                <span className='text-muted-foreground ms-1'>(1-100)</span>
              </Label>
              <Input
                id='staff-ic-count'
                type='number'
                inputMode='numeric'
                min={1}
                max={100}
                value={staffCount}
                onChange={(e) => setStaffCount(e.target.value)}
                disabled={generating}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='staff-ic-max-uses' className='text-xs'>
                {t('Max uses per code')}
                <span className='text-muted-foreground ms-1'>
                  ({t('0 = unlimited, blank = default')})
                </span>
              </Label>
              <Input
                id='staff-ic-max-uses'
                type='number'
                inputMode='numeric'
                min={0}
                placeholder={String(quota?.default_code_max_uses ?? '')}
                value={staffMaxUses}
                onChange={(e) => setStaffMaxUses(e.target.value)}
                disabled={generating}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor='staff-ic-valid-days' className='text-xs'>
                {t('Valid days')}
                <span className='text-muted-foreground ms-1'>
                  ({t('0 = permanent, blank = default')})
                </span>
              </Label>
              <Input
                id='staff-ic-valid-days'
                type='number'
                inputMode='numeric'
                min={0}
                placeholder={String(quota?.default_code_valid_days ?? '')}
                value={staffValidDays}
                onChange={(e) => setStaffValidDays(e.target.value)}
                disabled={generating}
              />
            </div>
          </div>
        ) : null}

        <div className='bg-card divide-y overflow-hidden rounded-xl border sm:hidden'>
          {loading ? (
            Array.from({ length: 3 }).map((_, i) => (
              <div key={i} className='space-y-2 p-4'>
                <Skeleton className='h-5 w-2/3' />
                <Skeleton className='h-10 w-full' />
              </div>
            ))
          ) : codes.length === 0 ? (
            <div className='text-muted-foreground p-12 text-center text-sm'>
              {t('No invitation codes yet')}
            </div>
          ) : (
            codes.map((code) => (
              <article key={code.id} className='space-y-3 p-4'>
                <div className='flex items-start justify-between gap-3'>
                  <code className='min-w-0 font-mono text-sm font-medium break-all'>
                    {code.code}
                  </code>
                  <div className='shrink-0'>{renderCodeStatus(code)}</div>
                </div>
                <div className='grid grid-cols-2 gap-x-4 gap-y-3 text-sm'>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Uses')}
                    </div>
                    <div className='mt-0.5 tabular-nums'>
                      {code.max_uses > 0
                        ? `${code.used_count} / ${code.max_uses}`
                        : `${code.used_count} / ∞`}
                    </div>
                  </div>
                  <div>
                    <div className='text-muted-foreground text-xs'>
                      {t('Expires')}
                    </div>
                    <div className='mt-0.5 text-xs'>
                      {code.expired_time
                        ? formatDate(code.expired_time)
                        : t('Permanent')}
                    </div>
                  </div>
                  <div className='col-span-2'>
                    <div className='text-muted-foreground text-xs'>
                      {t('Created')}
                    </div>
                    <div className='mt-0.5 text-xs'>
                      {formatDate(code.created_time)}
                    </div>
                  </div>
                </div>
                <div className='grid grid-cols-2 gap-2'>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => onCopy(code.code)}
                  >
                    <Copy className='mr-1 h-3.5 w-3.5' />
                    {t('Copy')}
                  </Button>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => onCopyLink(code.code)}
                  >
                    <Link2 className='mr-1 h-3.5 w-3.5' />
                    {t('Link')}
                  </Button>
                </div>
              </article>
            ))
          )}
        </div>

        <div className='bg-card hidden overflow-hidden rounded-xl border sm:block'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Code')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
                <TableHead>{t('Uses')}</TableHead>
                <TableHead>{t('Created')}</TableHead>
                <TableHead>{t('Expires')}</TableHead>
                <TableHead className='text-right'>{t('Actions')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow>
                  <TableCell colSpan={6}>
                    <Skeleton className='h-6 w-full' />
                  </TableCell>
                </TableRow>
              ) : codes.length === 0 ? (
                <TableRow>
                  <TableCell
                    colSpan={6}
                    className='text-muted-foreground text-center text-sm'
                  >
                    {t('No invitation codes yet')}
                  </TableCell>
                </TableRow>
              ) : (
                codes.map((c) => {
                  const used = c.used_count
                  const max = c.max_uses
                  return (
                    <TableRow key={c.id}>
                      <TableCell className='font-mono'>{c.code}</TableCell>
                      <TableCell>{renderCodeStatus(c)}</TableCell>
                      <TableCell className='tabular-nums'>
                        {max > 0 ? `${used} / ${max}` : `${used} / ∞`}
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatDate(c.created_time)}
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {c.expired_time
                          ? formatDate(c.expired_time)
                          : t('Permanent')}
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          onClick={() => onCopy(c.code)}
                        >
                          <Copy className='mr-1 h-3.5 w-3.5' />
                          {t('Copy')}
                        </Button>
                        <Button
                          type='button'
                          variant='ghost'
                          size='sm'
                          onClick={() => onCopyLink(c.code)}
                        >
                          <Link2 className='mr-1 h-3.5 w-3.5' />
                          {t('Link')}
                        </Button>
                      </TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </div>
      </div>
    </div>
  )
}
