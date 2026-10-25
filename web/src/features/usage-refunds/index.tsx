import { useState } from 'react'
import { isAxiosError } from 'axios'
import { useQueryClient } from '@tanstack/react-query'
import { endOfDay, format, startOfDay } from 'date-fns'
import { Download, History, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getRequestErrorMessage } from '@/lib/api'
import {
  formatLogQuota,
  getEditableQuotaStep,
  quotaUnitsToEditableAmount,
} from '@/lib/format'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { BillingAnomalyPanel } from './components/billing-anomaly-panel'
import {
  downloadRefund,
  executeRefund,
  getRefund,
  listRefundCandidates,
  listRefunds,
  manualRefundSelection,
  parseExcludeGroups,
  parseRequestIDs,
  REFUND_EXCLUDE_GROUPS_KEY,
  refundQuotaFromInput,
  previewRefund,
  refundFilter,
  type RefundBatch,
  type RefundCandidates,
  type RefundSelection,
  type RefundView,
} from './lib/refunds'

const statusLabels: Record<string, string> = {
  eligible: 'Eligible',
  already_refunded: 'Already refunded',
  client_gone: 'Client disconnected',
  not_charged: 'No charge',
  separate_billing: 'Separate billing',
  invalid_metadata: 'Invalid metadata',
  user_unavailable: 'User unavailable',
  permission_denied: 'Permission denied',
  missing: 'Not found',
}

export function UsageRefunds() {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const [mode, setMode] = useState('filters')
  const [start, setStart] = useState(() =>
    format(startOfDay(new Date()), "yyyy-MM-dd'T'HH:mm:ss")
  )
  const [end, setEnd] = useState(() =>
    format(endOfDay(new Date()), "yyyy-MM-dd'T'HH:mm:ss")
  )
  const [filterFields, setFilterFields] = useState({
    group: '',
    channel: '',
    username: '',
    model_name: '',
    token_name: '',
    request_id: '',
    upstream_request_id: '',
    user_id: '',
  })
  const [itemFilter, setItemFilter] = useState('eligible')
  const [reason, setReason] = useState('')
  const [includeGone, setIncludeGone] = useState(false)
  const [lowOutput, setLowOutput] = useState(true)
  const [maxOutput, setMaxOutput] = useState('1')
  const [excludeGroups, setExcludeGroups] = useState(
    () => localStorage.getItem(REFUND_EXCLUDE_GROUPS_KEY) ?? ''
  )
  // Log IDs handed over from the billing anomaly tab.
  const [anomalyLogIDs, setAnomalyLogIDs] = useState<number[]>([])
  const [view, setView] = useState<RefundView | null>(null)
  const [history, setHistory] = useState<RefundBatch[] | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const [error, setError] = useState('')
  const [page, setPage] = useState(0)
  const [manualFields, setManualFields] = useState({
    user_id: '',
    username: '',
    model_name: '',
    request_ids: '',
  })
  const [candidates, setCandidates] = useState<RefundCandidates | null>(null)
  // log_id -> amount typed in the display currency; '' returns the full charge.
  const [picks, setPicks] = useState<Record<number, string>>({})
  const [candidatePage, setCandidatePage] = useState(0)
  const eligibleCandidates =
    candidates?.items.filter((item) => item.status === 'eligible') ?? []
  const pickedCount = Object.keys(picks).length
  const visibleItems =
    view?.items.filter(
      (item) =>
        itemFilter === 'all' ||
        (itemFilter === 'eligible'
          ? item.status === 'eligible'
          : item.status !== 'eligible')
    ) ?? []

  const work = async (
    action: () => Promise<void>,
    fallback = 'Failed to load batch'
  ) => {
    setBusy(true)
    setError('')
    try {
      await action()
    } catch (err) {
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
      setBusy(false)
    }
  }
  const rangeTimestamps = () => {
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
    return { startTimestamp, endTimestamp }
  }
  const resetCandidates = () => {
    setCandidates(null)
    setPicks({})
    setCandidatePage(0)
  }
  const loadCandidates = () =>
    work(async () => {
      const { startTimestamp, endTimestamp } = rangeTimestamps()
      const userID = manualFields.user_id.trim()
      const username = manualFields.username.trim()
      if (!userID === !username)
        throw new Error('Enter either a user ID or a username')
      if (
        userID &&
        (!/^\d+$/.test(userID) || !Number.isSafeInteger(Number(userID)))
      )
        throw new Error('Invalid filter ID')
      const requestIDs = parseRequestIDs(manualFields.request_ids)
      resetCandidates()
      const loaded = await listRefundCandidates({
        ...(userID ? { user_id: Number(userID) } : { username }),
        start_timestamp: startTimestamp,
        end_timestamp: endTimestamp,
        model_name: manualFields.model_name.trim(),
        request_ids: requestIDs,
        include_client_gone: includeGone,
      })
      setCandidates(loaded)
      // Pasted request IDs are the operator's pick list: preselect the eligible ones.
      if (requestIDs.length)
        setPicks(
          Object.fromEntries(
            loaded.items
              .filter((item) => item.status === 'eligible')
              .map((item) => [item.log_id, ''])
          )
        )
    }, 'Failed to load refund candidates')
  const togglePick = (logID: number, checked: boolean) =>
    setPicks((current) => {
      const next = { ...current }
      if (checked) next[logID] = next[logID] ?? ''
      else delete next[logID]
      return next
    })
  const showView = (next: RefundView) => {
    setView(next)
    setHistory(null)
    setConfirmed(false)
    setPage(0)
    setItemFilter(next.count ? 'eligible' : 'skipped')
  }
  const preview = () =>
    work(async () => {
      const selection: RefundSelection = {
        reason,
        include_client_gone: includeGone,
      }
      if (mode === 'filters') {
        const max = Number(maxOutput)
        if (
          lowOutput &&
          (!Number.isSafeInteger(max) || max < 0 || maxOutput === '')
        )
          throw new Error('Invalid output token limit')
        const { startTimestamp, endTimestamp } = rangeTimestamps()
        for (const value of [filterFields.channel, filterFields.user_id]) {
          if (
            value &&
            (!/^\d+$/.test(value) || !Number.isSafeInteger(Number(value)))
          )
            throw new Error('Invalid filter ID')
        }
        const excluded = parseExcludeGroups(excludeGroups)
        localStorage.setItem(REFUND_EXCLUDE_GROUPS_KEY, excluded.join(', '))
        selection.filter = {
          ...refundFilter(
            {
              ...filterFields,
              start_timestamp: startTimestamp,
              end_timestamp: endTimestamp,
              channel: Number(filterFields.channel),
              user_id: Number(filterFields.user_id),
            },
            lowOutput ? max : undefined
          ),
          upstream_request_id: filterFields.upstream_request_id,
          ...(excluded.length ? { exclude_groups: excluded } : {}),
        }
      } else if (mode === 'manual') {
        Object.assign(
          selection,
          manualRefundSelection(candidates?.items ?? [], picks)
        )
      } else {
        if (!anomalyLogIDs.length) throw new Error('Select at least one record')
        selection.log_ids = anomalyLogIDs
      }
      showView(await previewRefund(selection))
    }, 'Refund preview failed; no balance was changed')
  const execute = () =>
    work(async () => {
      if (!view || !confirmed) return
      try {
        showView(await executeRefund(view))
      } catch (err) {
        // A lost HTTP response may follow a committed transaction. Recover the
        // same batch before allowing another action.
        try {
          const current = await getRefund(view.id)
          showView(current)
          if (current.status !== 'executed') throw err
        } catch {
          throw err
        }
      }
      toast.success(t('Balance returned'))
      await Promise.all(
        ['logs', 'account-summary', 'personal-refunds'].map((key) =>
          queryClient.invalidateQueries({ queryKey: [key] })
        )
      )
    }, 'refund operation failed; query the batch before retrying')
  const loadHistory = () =>
    work(async () => {
      setHistory(await listRefunds())
      setView(null)
    }, 'Failed to load batches')

  return (
    <section className='flex min-w-0 flex-col gap-4 p-4 sm:p-6'>
      <header className='flex flex-col gap-1'>
        <h2 className='text-lg font-semibold'>{t('Batch balance return')}</h2>
        {view && (
          <p className='text-muted-foreground text-sm break-words'>
            {view.reason}
          </p>
        )}
      </header>
      {error && (
        <Alert variant='destructive'>
          <AlertDescription className='break-words'>{error}</AlertDescription>
        </Alert>
      )}
      {!view && !history && (
        <fieldset disabled={busy} className='flex min-w-0 flex-col gap-4'>
          <Tabs value={mode} onValueChange={setMode}>
            <TabsList className='flex h-auto flex-wrap'>
              <TabsTrigger value='filters'>
                {t('Filter conditions')}
              </TabsTrigger>
              <TabsTrigger value='manual'>{t('Manual return')}</TabsTrigger>
              <TabsTrigger value='anomalies'>
                {t('Billing anomalies')}
              </TabsTrigger>
            </TabsList>
          </Tabs>
          {mode === 'anomalies' && (
            <BillingAnomalyPanel
              disabled={busy}
              onRefund={(logIDs) => {
                // 按日志 ID 预览；异常记录多为客户端断开，默认一并纳入。
                setAnomalyLogIDs(logIDs)
                setIncludeGone(true)
                toast.info(
                  t('{{count}} records added; enter a reason and preview', {
                    count: logIDs.length,
                  })
                )
              }}
            />
          )}
          {mode === 'filters' && (
            <>
              <div className='grid min-w-0 gap-4 sm:grid-cols-2 lg:grid-cols-3'>
                <div className='flex min-w-0 flex-col gap-2'>
                  <Label htmlFor='refund-start'>{t('Start time')}</Label>
                  <Input
                    id='refund-start'
                    type='datetime-local'
                    step={1}
                    value={start}
                    onChange={(e) => setStart(e.target.value)}
                    className='max-w-full min-w-0'
                  />
                </div>
                <div className='flex min-w-0 flex-col gap-2'>
                  <Label htmlFor='refund-end'>{t('End time')}</Label>
                  <Input
                    id='refund-end'
                    type='datetime-local'
                    step={1}
                    value={end}
                    onChange={(e) => setEnd(e.target.value)}
                    className='max-w-full min-w-0'
                  />
                </div>
                {(
                  [
                    ['group', 'Group'],
                    ['channel', 'Channel ID'],
                    ['username', 'Username'],
                    ['user_id', 'User ID'],
                    ['model_name', 'Model'],
                    ['token_name', 'Token name'],
                    ['request_id', 'Request ID'],
                    ['upstream_request_id', 'Upstream request ID'],
                  ] as const
                ).map(([key, label]) => (
                  <div key={key} className='flex min-w-0 flex-col gap-2'>
                    <Label htmlFor={`refund-${key}`}>{t(label)}</Label>
                    <Input
                      id={`refund-${key}`}
                      value={filterFields[key]}
                      type={
                        key === 'channel' || key === 'user_id'
                          ? 'number'
                          : 'text'
                      }
                      min={0}
                      step={1}
                      onChange={(e) =>
                        setFilterFields((current) => ({
                          ...current,
                          [key]: e.target.value,
                        }))
                      }
                    />
                  </div>
                ))}
              </div>
              <div className='flex min-w-0 flex-col gap-2'>
                <Label htmlFor='refund-exclude-groups'>
                  {t('Excluded groups')}
                </Label>
                <Input
                  id='refund-exclude-groups'
                  value={excludeGroups}
                  placeholder='0_Tools_Knowledge_Base, 1_vip_typesafe'
                  onChange={(e) => setExcludeGroups(e.target.value)}
                />
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Separate with commas or spaces. Logs in these groups are left out of the scan; the list is remembered in this browser.'
                  )}
                </p>
              </div>
              <div className='flex flex-wrap items-center gap-3'>
                <Label>
                  <Checkbox
                    checked={lowOutput}
                    onCheckedChange={(value) => setLowOutput(value === true)}
                  />
                  {t('Maximum output tokens')}
                </Label>
                <Input
                  aria-label={t('Maximum output tokens')}
                  type='number'
                  min={0}
                  step={1}
                  value={maxOutput}
                  disabled={!lowOutput}
                  onChange={(e) => setMaxOutput(e.target.value)}
                  className='w-28'
                />
              </div>
            </>
          )}
          {mode === 'manual' && (
            <>
              <div className='grid min-w-0 gap-4 sm:grid-cols-2 lg:grid-cols-3'>
                {(
                  [
                    ['start', 'Start time', start, setStart],
                    ['end', 'End time', end, setEnd],
                  ] as const
                ).map(([key, label, value, setValue]) => (
                  <div key={key} className='flex min-w-0 flex-col gap-2'>
                    <Label htmlFor={`manual-refund-${key}`}>{t(label)}</Label>
                    <Input
                      id={`manual-refund-${key}`}
                      type='datetime-local'
                      step={1}
                      value={value}
                      onChange={(e) => {
                        setValue(e.target.value)
                        resetCandidates()
                      }}
                      className='max-w-full min-w-0'
                    />
                  </div>
                ))}
                {(
                  [
                    ['user_id', 'User ID'],
                    ['username', 'Username'],
                    ['model_name', 'Model'],
                  ] as const
                ).map(([key, label]) => (
                  <div key={key} className='flex min-w-0 flex-col gap-2'>
                    <Label htmlFor={`manual-refund-${key}`}>{t(label)}</Label>
                    <Input
                      id={`manual-refund-${key}`}
                      value={manualFields[key]}
                      type={key === 'user_id' ? 'number' : 'text'}
                      min={0}
                      step={1}
                      onChange={(e) => {
                        setManualFields((current) => ({
                          ...current,
                          [key]: e.target.value,
                        }))
                        resetCandidates()
                      }}
                    />
                  </div>
                ))}
              </div>
              <div className='flex min-w-0 flex-col gap-2'>
                <Label htmlFor='manual-refund-request_ids'>
                  {t('Request ID')}
                </Label>
                <Textarea
                  id='manual-refund-request_ids'
                  value={manualFields.request_ids}
                  placeholder={t(
                    'Optional. Paste one or more request IDs, e.g. the RequestID column copied from a usage export'
                  )}
                  onChange={(e) => {
                    setManualFields((current) => ({
                      ...current,
                      request_ids: e.target.value,
                    }))
                    resetCandidates()
                  }}
                  rows={3}
                  className='font-mono text-xs'
                />
              </div>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <span className='text-muted-foreground text-sm'>
                  {candidates
                    ? `${candidates.username} (#${candidates.user_id}) · ${t('Selected')}: ${pickedCount} / ${eligibleCandidates.length}`
                    : t('Exact username or user ID; at most 31 days')}
                </span>
                <Button variant='outline' onClick={() => void loadCandidates()}>
                  {t('Load consumption logs')}
                </Button>
              </div>
              {!!candidates?.missing_request_ids?.length && (
                <Alert>
                  <AlertDescription className='break-all'>
                    {t(
                      '{{count}} request IDs have no charged log for this user in the time range',
                      { count: candidates.missing_request_ids.length }
                    )}
                    : {candidates.missing_request_ids.join(', ')}
                  </AlertDescription>
                </Alert>
              )}
              {candidates && (
                <>
                  <div className='max-h-96 overflow-auto'>
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead className='w-10'>
                            <Checkbox
                              aria-label={t('Select all')}
                              checked={
                                eligibleCandidates.length > 0 &&
                                pickedCount === eligibleCandidates.length
                              }
                              disabled={!eligibleCandidates.length}
                              onCheckedChange={(value) =>
                                setPicks(
                                  value === true
                                    ? Object.fromEntries(
                                        eligibleCandidates.map((item) => [
                                          item.log_id,
                                          picks[item.log_id] ?? '',
                                        ])
                                      )
                                    : {}
                                )
                              }
                            />
                          </TableHead>
                          <TableHead>{t('Time')}</TableHead>
                          <TableHead>{t('Model')}</TableHead>
                          <TableHead>{t('Request ID')}</TableHead>
                          <TableHead>{t('Original charge')}</TableHead>
                          <TableHead className='min-w-36'>
                            {t('Return amount')}
                          </TableHead>
                          <TableHead>{t('Status')}</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {candidates.items
                          .slice(candidatePage * 50, (candidatePage + 1) * 50)
                          .map((item) => {
                            const picked = item.log_id in picks
                            const typed = picks[item.log_id] ?? ''
                            const quota = refundQuotaFromInput(item, typed)
                            return (
                              <TableRow key={item.log_id}>
                                <TableCell>
                                  <Checkbox
                                    aria-label={String(item.log_id)}
                                    checked={picked}
                                    disabled={item.status !== 'eligible'}
                                    onCheckedChange={(value) =>
                                      togglePick(item.log_id, value === true)
                                    }
                                  />
                                </TableCell>
                                <TableCell className='whitespace-nowrap'>
                                  {new Date(
                                    item.created_at * 1000
                                  ).toLocaleString()}
                                </TableCell>
                                <TableCell className='max-w-40 break-all whitespace-normal'>
                                  {item.model_name || '-'}
                                </TableCell>
                                <TableCell className='max-w-48 break-all whitespace-normal'>
                                  {item.request_id || '-'}
                                </TableCell>
                                <TableCell>
                                  {formatLogQuota(item.quota)}
                                </TableCell>
                                <TableCell>
                                  {picked ? (
                                    <div className='flex flex-col gap-1'>
                                      <Input
                                        aria-label={t('Return amount')}
                                        type='number'
                                        inputMode='decimal'
                                        min={0}
                                        step={getEditableQuotaStep()}
                                        placeholder={String(
                                          quotaUnitsToEditableAmount(item.quota)
                                        )}
                                        value={typed}
                                        onChange={(e) =>
                                          setPicks((current) => ({
                                            ...current,
                                            [item.log_id]: e.target.value,
                                          }))
                                        }
                                        className='w-32'
                                      />
                                      <span className='text-muted-foreground text-xs'>
                                        {quota !== null
                                          ? formatLogQuota(quota)
                                          : t('Invalid refund amount')}
                                      </span>
                                    </div>
                                  ) : (
                                    '-'
                                  )}
                                </TableCell>
                                <TableCell className='break-words whitespace-normal'>
                                  {item.status === 'eligible'
                                    ? t('Eligible')
                                    : t('Skipped: {{reason}}', {
                                        reason: t(
                                          statusLabels[item.status] ??
                                            item.status
                                        ),
                                      })}
                                  {item.refund_batch_id && (
                                    <div className='text-muted-foreground mt-1 text-xs break-all'>
                                      {t('Refund batch')}:{' '}
                                      {item.refund_batch_id}
                                      {item.refunded_quota
                                        ? ` · ${formatLogQuota(item.refunded_quota)}`
                                        : ''}
                                    </div>
                                  )}
                                </TableCell>
                              </TableRow>
                            )
                          })}
                        {!candidates.items.length && (
                          <TableRow>
                            <TableCell colSpan={7}>
                              {t('No records found')}
                            </TableCell>
                          </TableRow>
                        )}
                      </TableBody>
                    </Table>
                  </div>
                  {candidates.items.length > 50 && (
                    <div className='flex items-center justify-end gap-3'>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={candidatePage === 0}
                        onClick={() => setCandidatePage(candidatePage - 1)}
                      >
                        {t('Previous')}
                      </Button>
                      <span className='text-sm'>
                        {candidatePage + 1} /{' '}
                        {Math.ceil(candidates.items.length / 50)}
                      </span>
                      <Button
                        variant='outline'
                        size='sm'
                        disabled={
                          (candidatePage + 1) * 50 >= candidates.items.length
                        }
                        onClick={() => setCandidatePage(candidatePage + 1)}
                      >
                        {t('Next')}
                      </Button>
                    </div>
                  )}
                </>
              )}
            </>
          )}
          {mode === 'anomalies' && anomalyLogIDs.length > 0 && (
            <Alert>
              <AlertDescription className='flex flex-wrap items-center gap-2'>
                {t('{{count}} records added; enter a reason and preview', {
                  count: anomalyLogIDs.length,
                })}
                <Button
                  variant='link'
                  className='h-auto p-0'
                  onClick={() => setAnomalyLogIDs([])}
                >
                  {t('Clear')}
                </Button>
              </AlertDescription>
            </Alert>
          )}
          {(mode !== 'anomalies' || anomalyLogIDs.length > 0) && (
            <>
              <Label>
                <Checkbox
                  checked={includeGone}
                  onCheckedChange={(value) => {
                    setIncludeGone(value === true)
                    resetCandidates()
                  }}
                />
                {t('Include client disconnects')}
              </Label>
              <div className='flex flex-col gap-2'>
                <Label htmlFor='refund-reason'>{t('Refund reason')}</Label>
                <Textarea
                  id='refund-reason'
                  value={reason}
                  onChange={(e) => setReason(e.target.value)}
                  maxLength={150}
                  rows={2}
                />
              </div>
            </>
          )}
        </fieldset>
      )}
      {history && (
        <div className='max-h-96 overflow-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Created at')}</TableHead>
                <TableHead>{t('Refund reason')}</TableHead>
                <TableHead>{t('Amount')}</TableHead>
                <TableHead>{t('Status')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {history.map((batch) => (
                <TableRow key={batch.id}>
                  <TableCell>
                    <Button
                      variant='link'
                      disabled={busy}
                      onClick={() =>
                        void work(async () =>
                          showView(await getRefund(batch.id))
                        )
                      }
                    >
                      {new Date(batch.created_at * 1000).toLocaleString()}
                    </Button>
                  </TableCell>
                  <TableCell className='max-w-64 break-words whitespace-normal'>
                    {batch.reason}
                  </TableCell>
                  <TableCell>{formatLogQuota(batch.quota)}</TableCell>
                  <TableCell>
                    {t(
                      batch.status === 'executed'
                        ? 'Balance returned'
                        : 'Preview'
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!history.length && (
                <TableRow>
                  <TableCell colSpan={4}>{t('No records found')}</TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
      )}
      {view && (
        <>
          <dl className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
            {(
              [
                [t('Eligible'), `${view.count} / ${view.items.length}`],
                [t('Skipped'), String(view.items.length - view.count)],
                [t('Amount'), formatLogQuota(view.quota)],
                [
                  t('Status'),
                  t(
                    view.status === 'executed' ? 'Balance returned' : 'Preview'
                  ),
                ],
              ] as const
            ).map(([label, value]) => (
              <div key={label} className='rounded-md border px-3 py-2'>
                <dt className='text-muted-foreground text-xs'>{label}</dt>
                <dd className='mt-1 text-sm font-semibold tabular-nums'>
                  {value}
                </dd>
              </div>
            ))}
          </dl>
          <div className='text-muted-foreground font-mono text-xs break-all'>
            {view.id}
          </div>
          {view.logs_pending > 0 && (
            <Alert>
              <AlertDescription>
                {t('Balance returned; user logs pending delivery')}
                <Button
                  variant='link'
                  disabled={busy}
                  onClick={() =>
                    void work(async () => showView(await getRefund(view.id)))
                  }
                >
                  {t('Refresh')}
                </Button>
              </AlertDescription>
            </Alert>
          )}
          <div className='max-h-56 overflow-auto rounded-md border'>
            <Table>
              <TableHeader className='bg-background sticky top-0'>
                <TableRow>
                  <TableHead>{t('Username')}</TableHead>
                  <TableHead className='w-24 text-end'>{t('Count')}</TableHead>
                  <TableHead className='w-36 text-end'>{t('Amount')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {view.users.map((user) => (
                  <TableRow key={user.user_id}>
                    <TableCell className='break-all whitespace-normal'>
                      {user.username}{' '}
                      <span className='text-muted-foreground'>
                        #{user.user_id}
                      </span>
                    </TableCell>
                    <TableCell className='text-end tabular-nums'>
                      {user.count}
                    </TableCell>
                    <TableCell className='text-end tabular-nums'>
                      {formatLogQuota(user.quota)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
          <Tabs
            value={itemFilter}
            onValueChange={(value) => {
              setItemFilter(value)
              setPage(0)
            }}
          >
            <TabsList className='flex h-auto flex-wrap'>
              <TabsTrigger value='eligible'>
                {t(
                  view.status === 'executed' ? 'Balance returned' : 'Eligible'
                )}{' '}
                ({view.count})
              </TabsTrigger>
              <TabsTrigger value='skipped'>
                {t('Skipped')} ({view.items.length - view.count})
              </TabsTrigger>
              <TabsTrigger value='all'>
                {t('All')} ({view.items.length})
              </TabsTrigger>
            </TabsList>
          </Tabs>
          <div className='max-h-[32rem] overflow-auto rounded-md border'>
            <Table className='min-w-[56rem]'>
              <TableHeader className='bg-background sticky top-0'>
                <TableRow>
                  <TableHead className='w-24'>{t('Log ID')}</TableHead>
                  <TableHead className='w-40'>{t('Time')}</TableHead>
                  <TableHead className='w-40'>{t('User')}</TableHead>
                  <TableHead className='w-44'>{t('Model')}</TableHead>
                  <TableHead>{t('Request ID')}</TableHead>
                  <TableHead className='w-32 text-end'>{t('Amount')}</TableHead>
                  <TableHead className='w-40'>{t('Status')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {visibleItems
                  .slice(page * 50, (page + 1) * 50)
                  .map((item, i) => (
                    <TableRow key={`${item.log_id}-${i}`}>
                      <TableCell className='tabular-nums'>
                        {item.log_id || '-'}
                      </TableCell>
                      <TableCell className='tabular-nums'>
                        {item.created_at
                          ? format(
                              new Date(item.created_at * 1000),
                              'MM-dd HH:mm:ss'
                            )
                          : '-'}
                      </TableCell>
                      <TableCell className='break-all whitespace-normal'>
                        {item.user_id ? (
                          <>
                            {item.username}{' '}
                            <span className='text-muted-foreground'>
                              #{item.user_id}
                            </span>
                          </>
                        ) : (
                          '-'
                        )}
                      </TableCell>
                      <TableCell className='break-all whitespace-normal'>
                        {item.model_name || '-'}
                      </TableCell>
                      <TableCell className='font-mono text-xs break-all whitespace-normal'>
                        {item.request_id || '-'}
                      </TableCell>
                      <TableCell className='text-end tabular-nums'>
                        {formatLogQuota(item.quota)}
                        {!!item.original_quota &&
                          item.original_quota !== item.quota && (
                            <div className='text-muted-foreground text-xs'>
                              {t('Original charge')}:{' '}
                              {formatLogQuota(item.original_quota)}
                            </div>
                          )}
                      </TableCell>
                      <TableCell className='break-words whitespace-normal'>
                        <span>
                          {item.status === 'eligible'
                            ? t(
                                view.status === 'executed'
                                  ? 'Balance returned'
                                  : 'Eligible'
                              )
                            : t('Skipped: {{reason}}', {
                                reason: t(
                                  statusLabels[item.status] ?? item.status
                                ),
                              })}
                        </span>
                        {item.refund_batch_id && (
                          <div className='text-muted-foreground mt-1 text-xs break-all'>
                            {t('Refund batch')}: {item.refund_batch_id}
                          </div>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                {!visibleItems.length && (
                  <TableRow>
                    <TableCell colSpan={7}>{t('No records found')}</TableCell>
                  </TableRow>
                )}
              </TableBody>
            </Table>
          </div>
          {visibleItems.length > 50 && (
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
                {page + 1} / {Math.ceil(visibleItems.length / 50)}
              </span>
              <Button
                variant='outline'
                size='sm'
                disabled={(page + 1) * 50 >= visibleItems.length}
                onClick={() => setPage(page + 1)}
              >
                {t('Next')}
              </Button>
            </div>
          )}
          {view.status === 'preview' && view.count > 0 && (
            <Label className='flex items-start gap-2'>
              <Checkbox
                checked={confirmed}
                disabled={busy}
                onCheckedChange={(value) => setConfirmed(value === true)}
              />
              {t('Confirm returning {{amount}} for {{count}} records', {
                amount: formatLogQuota(view.quota),
                count: view.count,
              })}
            </Label>
          )}
        </>
      )}
      <div className='flex flex-wrap items-center justify-end gap-2'>
        <Button
          variant='ghost'
          disabled={busy}
          onClick={() => void loadHistory()}
        >
          <History data-icon='inline-start' />
          {t('Refund batches')}
        </Button>
        {(view || history) && (
          <Button
            variant='outline'
            disabled={busy}
            onClick={() => {
              setView(null)
              setHistory(null)
              setError('')
            }}
          >
            {t('Back')}
          </Button>
        )}
        {view && (
          <Button
            variant='outline'
            disabled={busy}
            onClick={() => downloadRefund(view)}
          >
            <Download data-icon='inline-start' />
            {t('Export')}
          </Button>
        )}
        {!view &&
          !history &&
          (mode !== 'anomalies' || anomalyLogIDs.length > 0) && (
            <Button
              disabled={
                busy || !reason.trim() || (mode === 'manual' && !pickedCount)
              }
              onClick={() => void preview()}
            >
              {busy && <Loader2 className='animate-spin' />}
              {t('Preview')}
            </Button>
          )}
        {view?.status === 'preview' && view.count > 0 && (
          <Button disabled={busy || !confirmed} onClick={() => void execute()}>
            {busy && <Loader2 className='animate-spin' />}
            {t('Return balance')}
          </Button>
        )}
      </div>
    </section>
  )
}
