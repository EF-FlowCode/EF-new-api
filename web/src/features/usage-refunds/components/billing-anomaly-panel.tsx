// eflow: 异常计费审核 tab。按 logs.billing_anomaly 列出计费用量不可靠的消费日志：
// 估算扣费（1/2）可勾选后转入批量返还预览；上游无用量（3）已按 0 结算免单，仅供审核。
// 「扣费但输出 ≤1」不看标记，用来找标记上线（2026-09-28）前的同类异常。
import { useState } from 'react'
import { isAxiosError } from 'axios'
import { endOfDay, format, startOfDay } from 'date-fns'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { getRequestErrorMessage } from '@/lib/api'
import { formatLogQuota } from '@/lib/format'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import {
  BILLING_ANOMALY,
  isRefundableAnomaly,
  listBillingAnomalies,
  type BillingAnomalies,
} from '../lib/refunds'

const anomalyLabels: Record<number, string> = {
  [BILLING_ANOMALY.inputEstimated]: 'Input estimated',
  [BILLING_ANOMALY.outputEstimated]: 'Output estimated',
  [BILLING_ANOMALY.noUpstreamUsage]: 'No upstream usage (free)',
}

const PAGE_SIZE = 50
// Select value for "charged with at most 1 output token", sent as low_output.
const LOW_OUTPUT = 'low_output'

interface BillingAnomalyPanelProps {
  disabled: boolean
  // Hands selected log IDs to the batch refund preview (client disconnects included).
  onRefund: (logIDs: number[]) => void
}

export function BillingAnomalyPanel({
  disabled,
  onRefund,
}: BillingAnomalyPanelProps) {
  const { t } = useTranslation()
  const [start, setStart] = useState(() =>
    format(startOfDay(new Date()), "yyyy-MM-dd'T'HH:mm:ss")
  )
  const [end, setEnd] = useState(() =>
    format(endOfDay(new Date()), "yyyy-MM-dd'T'HH:mm:ss")
  )
  const [anomaly, setAnomaly] = useState(String(BILLING_ANOMALY.inputEstimated))
  const [fields, setFields] = useState({
    user_id: '',
    channel: '',
    model_name: '',
  })
  const [view, setView] = useState<'users' | 'items'>('items')
  const [data, setData] = useState<BillingAnomalies | null>(null)
  const [picked, setPicked] = useState<Set<number>>(new Set())
  const [page, setPage] = useState(0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const refundable = data?.items.filter(isRefundableAnomaly) ?? []
  const pickedQuota = refundable
    .filter((item) => picked.has(item.log_id))
    .reduce((sum, item) => sum + item.quota, 0)

  const load = async (override?: { user_id?: string }) => {
    setLoading(true)
    setError('')
    try {
      const startTimestamp = new Date(start).getTime() / 1000
      const endTimestamp = new Date(end).getTime() / 1000
      if (
        !Number.isFinite(startTimestamp) ||
        !Number.isFinite(endTimestamp) ||
        startTimestamp <= 0 ||
        endTimestamp < startTimestamp ||
        endTimestamp - startTimestamp > 31 * 86400
      )
        throw new Error('Invalid refund time range (maximum 31 days)')
      const userID = (override?.user_id ?? fields.user_id).trim()
      for (const value of [userID, fields.channel.trim()]) {
        if (
          value &&
          (!/^\d+$/.test(value) || !Number.isSafeInteger(Number(value)))
        )
          throw new Error('Invalid filter ID')
      }
      setData(
        await listBillingAnomalies({
          start_timestamp: startTimestamp,
          end_timestamp: endTimestamp,
          anomaly: anomaly === LOW_OUTPUT ? 0 : Number(anomaly),
          low_output: anomaly === LOW_OUTPUT,
          user_id: userID ? Number(userID) : undefined,
          channel: fields.channel.trim() ? Number(fields.channel) : undefined,
          model_name: fields.model_name.trim(),
        })
      )
      setPicked(new Set())
      setPage(0)
    } catch (err) {
      const fallback = 'Failed to load billing anomalies'
      const responseMessage = isAxiosError<{ message?: unknown }>(err)
        ? err.response?.data?.message
        : undefined
      setError(
        t(
          isAxiosError(err) &&
            (typeof responseMessage !== 'string' || !responseMessage)
            ? fallback
            : getRequestErrorMessage(err, fallback)
        )
      )
    } finally {
      setLoading(false)
    }
  }

  const drillIntoUser = (userID: number) => {
    setFields((current) => ({ ...current, user_id: String(userID) }))
    setView('items')
    void load({ user_id: String(userID) })
  }

  const togglePick = (logID: number, checked: boolean) =>
    setPicked((current) => {
      const next = new Set(current)
      if (checked) next.add(logID)
      else next.delete(logID)
      return next
    })

  const items = data?.items ?? []
  const pageItems = items.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)

  return (
    <fieldset
      disabled={disabled || loading}
      className='flex min-w-0 flex-col gap-4'
    >
      <Alert>
        <AlertDescription className='break-words'>
          {t(
            'Estimated charges can be moved into a refund batch. Requests with no upstream usage were settled at zero and are listed for audit only.'
          )}
        </AlertDescription>
      </Alert>
      {error && (
        <Alert variant='destructive'>
          <AlertDescription className='break-words'>{error}</AlertDescription>
        </Alert>
      )}
      <div className='grid min-w-0 gap-4 sm:grid-cols-2 lg:grid-cols-3'>
        {(
          [
            ['start', 'Start time', start, setStart],
            ['end', 'End time', end, setEnd],
          ] as const
        ).map(([key, label, value, setValue]) => (
          <div key={key} className='flex min-w-0 flex-col gap-2'>
            <Label htmlFor={`anomaly-${key}`}>{t(label)}</Label>
            <Input
              id={`anomaly-${key}`}
              type='datetime-local'
              step={1}
              value={value}
              onChange={(e) => setValue(e.target.value)}
              className='max-w-full min-w-0'
            />
          </div>
        ))}
        <div className='flex min-w-0 flex-col gap-2'>
          <Label htmlFor='anomaly-kind'>{t('Anomaly type')}</Label>
          <Select value={anomaly} onValueChange={setAnomaly}>
            <SelectTrigger id='anomaly-kind'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value='0'>{t('All anomalies')}</SelectItem>
                {Object.entries(anomalyLabels).map(([value, label]) => (
                  <SelectItem key={value} value={value}>
                    {t(label)}
                  </SelectItem>
                ))}
                <SelectItem value={LOW_OUTPUT}>
                  {t('Charged with output ≤ 1 (incl. unflagged)')}
                </SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </div>
        {(
          [
            ['user_id', 'User ID'],
            ['channel', 'Channel ID'],
            ['model_name', 'Model'],
          ] as const
        ).map(([key, label]) => (
          <div key={key} className='flex min-w-0 flex-col gap-2'>
            <Label htmlFor={`anomaly-${key}`}>{t(label)}</Label>
            <Input
              id={`anomaly-${key}`}
              value={fields[key]}
              type={key === 'model_name' ? 'text' : 'number'}
              min={0}
              step={1}
              onChange={(e) =>
                setFields((current) => ({
                  ...current,
                  [key]: e.target.value,
                }))
              }
            />
          </div>
        ))}
      </div>
      <div className='flex flex-wrap items-center justify-between gap-2'>
        <Tabs
          value={view}
          onValueChange={(value) => setView(value as 'users' | 'items')}
        >
          <TabsList>
            <TabsTrigger value='items'>
              {t('Details')}
              {data ? ` (${data.items.length})` : ''}
            </TabsTrigger>
            <TabsTrigger value='users'>
              {t('By user')}
              {data ? ` (${data.users.length})` : ''}
            </TabsTrigger>
          </TabsList>
        </Tabs>
        <Button variant='outline' onClick={() => void load()}>
          {loading && <Loader2 className='size-3.5 animate-spin' />}
          {t('Query')}
        </Button>
      </div>

      {data && view === 'users' && (
        <div className='max-h-[32rem] overflow-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Count')}</TableHead>
                {Object.entries(anomalyLabels).map(([value, label]) => (
                  <TableHead key={value}>{t(label)}</TableHead>
                ))}
                <TableHead>{t('Charged')}</TableHead>
                <TableHead>{t('Returned')}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {data.users.map((user) => (
                <TableRow key={user.user_id}>
                  <TableCell className='break-all whitespace-normal'>
                    {user.username} (#{user.user_id})
                  </TableCell>
                  <TableCell>{user.count}</TableCell>
                  {Object.keys(anomalyLabels).map((value) => (
                    <TableCell key={value}>
                      {user.by_anomaly[value] ?? 0}
                    </TableCell>
                  ))}
                  <TableCell>{formatLogQuota(user.quota)}</TableCell>
                  <TableCell>
                    {user.refunded_quota
                      ? formatLogQuota(user.refunded_quota)
                      : '-'}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => drillIntoUser(user.user_id)}
                    >
                      {t('View details')}
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
              {!data.users.length && (
                <TableRow>
                  <TableCell colSpan={7}>{t('No records found')}</TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      )}

      {data && view === 'items' && (
        <>
          <div className='max-h-[32rem] overflow-auto'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-10'>
                    <Checkbox
                      aria-label={t('Select all')}
                      checked={
                        refundable.length > 0 &&
                        picked.size === refundable.length
                      }
                      disabled={!refundable.length}
                      onCheckedChange={(value) =>
                        setPicked(
                          value === true
                            ? new Set(refundable.map((item) => item.log_id))
                            : new Set()
                        )
                      }
                    />
                  </TableHead>
                  <TableHead>{t('Time')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Model')}</TableHead>
                  <TableHead>{t('Channel')}</TableHead>
                  <TableHead>{t('Input / Output')}</TableHead>
                  <TableHead>{t('Duration')}</TableHead>
                  <TableHead>{t('Charged')}</TableHead>
                  <TableHead>{t('Anomaly type')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {pageItems.map((item) => {
                  const canRefund = isRefundableAnomaly(item)
                  return (
                    <TableRow key={item.log_id}>
                      <TableCell>
                        <Checkbox
                          aria-label={String(item.log_id)}
                          checked={picked.has(item.log_id)}
                          disabled={!canRefund}
                          onCheckedChange={(value) =>
                            togglePick(item.log_id, value === true)
                          }
                        />
                      </TableCell>
                      <TableCell className='whitespace-nowrap'>
                        {new Date(item.created_at * 1000).toLocaleString()}
                      </TableCell>
                      <TableCell className='max-w-36 break-all whitespace-normal'>
                        {item.username} (#{item.user_id})
                      </TableCell>
                      <TableCell className='max-w-40 break-all whitespace-normal'>
                        {item.model_name || '-'}
                      </TableCell>
                      <TableCell>#{item.channel}</TableCell>
                      <TableCell className='whitespace-nowrap'>
                        {item.prompt_tokens.toLocaleString()} /{' '}
                        {item.completion_tokens.toLocaleString()}
                      </TableCell>
                      <TableCell>{item.use_time}s</TableCell>
                      <TableCell>{formatLogQuota(item.quota)}</TableCell>
                      <TableCell>
                        {item.anomaly ? (
                          <Badge
                            variant={
                              item.anomaly === BILLING_ANOMALY.inputEstimated
                                ? 'destructive'
                                : 'secondary'
                            }
                          >
                            {t(
                              anomalyLabels[item.anomaly] ??
                                String(item.anomaly)
                            )}
                          </Badge>
                        ) : (
                          <Badge variant='outline'>{t('Unflagged')}</Badge>
                        )}
                      </TableCell>
                      <TableCell className='break-words whitespace-normal'>
                        {item.anomaly === BILLING_ANOMALY.noUpstreamUsage
                          ? t('Audit only')
                          : item.status === 'eligible'
                            ? t('Eligible')
                            : t(
                                item.status === 'already_refunded'
                                  ? 'Already refunded'
                                  : 'Not refundable'
                              )}
                        {item.refund_batch_id && (
                          <div className='text-muted-foreground mt-1 text-xs break-all'>
                            {t('Refund batch')}: {item.refund_batch_id}
                            {item.refunded_quota
                              ? ` · ${formatLogQuota(item.refunded_quota)}`
                              : ''}
                          </div>
                        )}
                      </TableCell>
                    </TableRow>
                  )
                })}
                {!items.length && (
                  <TableRow>
                    <TableCell colSpan={10}>{t('No records found')}</TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
          {items.length > PAGE_SIZE && (
            <div className='flex items-center justify-end gap-3'>
              <Button
                variant='outline'
                size='sm'
                disabled={page === 0}
                onClick={() => setPage(page - 1)}
              >
                {t('Previous')}
              </Button>
              <span className='text-sm'>
                {page + 1} / {Math.ceil(items.length / PAGE_SIZE)}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={(page + 1) * PAGE_SIZE >= items.length}
                onClick={() => setPage(page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          )}
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <span className='text-muted-foreground text-sm'>
              {t('Selected')}: {picked.size} / {refundable.length} ·{' '}
              {formatLogQuota(pickedQuota)}
            </span>
            <Button
              disabled={!picked.size}
              onClick={() => onRefund([...picked].sort((a, b) => a - b))}
            >
              {t('Move to refund preview')}
            </Button>
          </div>
        </>
      )}
    </fieldset>
  )
}
