import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useAuthStore } from '@/stores/auth-store'
import { api } from '@/lib/api'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Form,
  FormControl,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Skeleton } from '@/components/ui/skeleton'
import { TASK_STATUS_MAPPINGS } from '../../constants'

type Price = {
  version: number
  mode: string
  base_quota: number
  resolution_ratios?: Record<string, number>
}
type Audit = {
  operation_id: string
  action: string
  reason: string
  evidence: string
  before_quota: number
  after_quota: number
  actor_id: number
}
export type Detail = {
  token_billing?: {
    version: number
    mode: string
    model_ratio?: number
    group_ratio?: number
    other_multiplier?: number
  }
  completion_tokens?: number
  id: number
  task_id: string
  status: string
  revision: number
  quota: number
  reason: string
  ready: boolean
  manual_quota?: boolean
  edit_seconds: boolean
  edit_resolution: boolean
  locked_until: number
  billing: Price | null
  seconds: number | null
  resolution: string | null
  audits?: Audit[]
}
type Preview = Audit & { expires: number }
const base = '/api/plugin/task/runtime'

async function reviewGet<T>(path: string): Promise<T> {
  const response = await api.get<{ data: T }>(`${base}${path}`)
  return response.data.data
}

export function ReviewForm({
  detail,
  done,
}: {
  detail: Detail
  done: () => void
}) {
  const { t } = useTranslation()
  const [preview, setPreview] = useState<Preview | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirmed, setConfirmed] = useState(false)
  const [error, setError] = useState(false)
  const pending = detail.status === 'BILLING_PENDING'
  const reviewable =
    pending || ['SUBMITTING', 'SUBMIT_UNKNOWN'].includes(detail.status)
  const form = useForm({
    defaultValues: {
      action: pending
        ? 'settle'
        : detail.status === 'SUBMITTING'
          ? 'refund'
          : 'attach',
      upstream_id: '',
      quota: '',
      seconds: '',
      resolution: '',
      price_mode: 'duration',
      base_quota: '',
      resolution_ratio: '',
      reason: '',
      evidence: '',
    },
  })
  const action = form.watch('action')
  const priceMode = form.watch('price_mode')
  const input = (
    name:
      | 'upstream_id'
      | 'quota'
      | 'seconds'
      | 'resolution'
      | 'base_quota'
      | 'resolution_ratio'
      | 'reason'
      | 'evidence',
    label: string,
    type = 'text'
  ) => (
    <FormField
      key={name}
      control={form.control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{t(label)}</FormLabel>
          <FormControl>
            <Input
              {...field}
              required={[
                'reason',
                'evidence',
                'upstream_id',
                'base_quota',
                'resolution_ratio',
              ].includes(name)}
              type={type}
              step={type === 'number' ? 'any' : undefined}
              maxLength={name === 'evidence' ? 4000 : 1000}
            />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  )
  const submit = form.handleSubmit(async (values) => {
    setBusy(true)
    setError(false)
    try {
      const payload = {
        revision: detail.revision,
        action: values.action,
        reason: values.reason,
        evidence: values.evidence,
        confirmed,
        ...(values.action === 'attach'
          ? { upstream_id: values.upstream_id }
          : {}),
        ...(values.action === 'settle' && values.quota !== ''
          ? { quota: Number(values.quota) }
          : {}),
        ...(values.action === 'settle' && values.quota === ''
          ? {
              ...(values.seconds !== ''
                ? { seconds: Number(values.seconds) }
                : {}),
              ...(values.resolution !== ''
                ? { resolution: values.resolution }
                : {}),
              ...(!detail.billing && !detail.token_billing
                ? {
                    billing: {
                      version: 1,
                      mode: values.price_mode,
                      base_quota: Number(values.base_quota),
                      resolution_ratios:
                        values.price_mode === 'duration'
                          ? {
                              [(
                                values.resolution ||
                                detail.resolution ||
                                ''
                              ).toUpperCase()]: Number(values.resolution_ratio),
                            }
                          : {},
                    },
                  }
                : {}),
            }
          : {}),
      }
      const response = await api.post<{ data: Preview }>(
        `${base}/tasks/${detail.id}/review/preview`,
        payload
      )
      setPreview(response.data.data)
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  })
  const confirm = async () => {
    if (!preview) return
    setBusy(true)
    setError(false)
    try {
      await api.post(
        `${base}/reviews/${encodeURIComponent(preview.operation_id)}/confirm`
      )
      done()
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className='flex flex-col gap-4'>
      <dl className='grid grid-cols-2 gap-2 text-sm'>
        <dt>{t('Task ID')}</dt>
        <dd className='break-all'>{detail.task_id}</dd>
        <dt>{t('Reserved quota')}</dt>
        <dd>{detail.quota}</dd>
        <dt>{t('Status')}</dt>
        <dd>
          {t(TASK_STATUS_MAPPINGS[detail.status]?.label || detail.status)}
        </dd>
        {detail.reason && (
          <>
            <dt>{t('Reason')}</dt>
            <dd className='break-all'>{detail.reason}</dd>
          </>
        )}
        {detail.resolution && (
          <>
            <dt>{t('Verified resolution')}</dt>
            <dd>{detail.resolution}</dd>
          </>
        )}
        {detail.token_billing?.mode === 'tokens' && (
          <>
            <dt>{t('Model ratio')}</dt>
            <dd>{detail.token_billing.model_ratio}</dd>
            <dt>{t('Group ratios')}</dt>
            <dd>{detail.token_billing.group_ratio}</dd>
            <dt>{t('Multiplier')}</dt>
            <dd>{detail.token_billing.other_multiplier}</dd>
          </>
        )}
        {detail.completion_tokens != null && (
          <>
            <dt>{t('Tokens')}</dt>
            <dd>{detail.completion_tokens}</dd>
          </>
        )}
        {detail.billing && (
          <>
            <dt>{t('Recorded unit quota')}</dt>
            <dd>
              {detail.billing.base_quota} ({detail.billing.mode})
            </dd>
          </>
        )}
        {detail.seconds != null && (
          <>
            <dt>{t('Verified duration (seconds)')}</dt>
            <dd>{detail.seconds}</dd>
          </>
        )}
      </dl>
      <Alert>
        <AlertDescription>
          {t(
            'Verify the provider records before reviewing. Finding a task ID alone does not prove it belongs to this request.'
          )}
        </AlertDescription>
      </Alert>
      {detail.locked_until > Date.now() / 1000 && (
        <p>{t('Submission is still being processed. Refresh later.')}</p>
      )}
      {error && (
        <p role='alert'>
          {t('Review failed. Refresh the task before trying again.')}
        </p>
      )}
      {!reviewable ? null : preview ? (
        <div className='flex flex-col gap-3'>
          <p>
            {t('Quota change')}: {preview.before_quota} → {preview.after_quota}{' '}
            ({preview.after_quota - preview.before_quota})
          </p>
          <p>
            {t('Reason')}: {preview.reason}
          </p>
          <p>
            {t('Evidence')}: {preview.evidence}
          </p>
          <p>
            {t('Preview expires at')}:{' '}
            {new Date(preview.expires * 1000).toLocaleString()}
          </p>
          <div className='flex justify-end gap-2'>
            <Button
              variant='outline'
              disabled={busy}
              onClick={() => setPreview(null)}
            >
              {t('Back')}
            </Button>
            <Button
              disabled={busy || preview.expires <= Date.now() / 1000}
              onClick={confirm}
            >
              {t('Confirm reviewed action')}
            </Button>
          </div>
        </div>
      ) : (
        <Form {...form}>
          <form onSubmit={submit} className='flex flex-col gap-4'>
            <FormField
              control={form.control}
              name='action'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Review action')}</FormLabel>
                  <Select value={field.value} onValueChange={field.onChange}>
                    <FormControl>
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      <SelectGroup>
                        {pending ? (
                          <>
                            <SelectItem value='settle'>
                              {t('Settle verified result')}
                            </SelectItem>
                            <SelectItem value='refund'>
                              {t('Refund reserved quota')}
                            </SelectItem>
                          </>
                        ) : (
                          <>
                            {detail.status !== 'SUBMITTING' && (
                              <SelectItem value='attach'>
                                {t('Recover provider task')}
                              </SelectItem>
                            )}
                            <SelectItem value='refund'>
                              {t('Confirm not accepted and refund')}
                            </SelectItem>
                          </>
                        )}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </FormItem>
              )}
            />
            {action === 'attach' && input('upstream_id', 'Provider task ID')}
            {action === 'settle' &&
              detail.manual_quota &&
              input('quota', 'Verified total quota', 'number')}
            {action === 'settle' && !detail.ready && (
              <>
                {detail.edit_seconds &&
                  input('seconds', 'Verified duration (seconds)', 'number')}
                {detail.edit_resolution &&
                  input('resolution', 'Verified resolution')}
                {!detail.billing && !detail.token_billing && (
                  <>
                    <FormField
                      control={form.control}
                      name='price_mode'
                      render={({ field }) => (
                        <FormItem>
                          <FormLabel>{t('Price basis')}</FormLabel>
                          <Select
                            value={field.value}
                            onValueChange={field.onChange}
                          >
                            <FormControl>
                              <SelectTrigger>
                                <SelectValue />
                              </SelectTrigger>
                            </FormControl>
                            <SelectContent>
                              <SelectGroup>
                                <SelectItem value='duration'>
                                  {t('Per second')}
                                </SelectItem>
                                <SelectItem value='fixed'>
                                  {t('Fixed total')}
                                </SelectItem>
                              </SelectGroup>
                            </SelectContent>
                          </Select>
                        </FormItem>
                      )}
                    />
                    {input(
                      'base_quota',
                      priceMode === 'duration'
                        ? 'Recorded unit quota'
                        : 'Verified total quota',
                      'number'
                    )}
                    {priceMode === 'duration' &&
                      input(
                        'resolution_ratio',
                        'Verified resolution multiplier',
                        'number'
                      )}
                  </>
                )}
              </>
            )}
            {input('reason', 'Reason')}
            {input('evidence', 'Evidence')}
            <label className='flex items-start gap-2 text-sm'>
              <Checkbox
                checked={confirmed}
                onCheckedChange={(v) => setConfirmed(v === true)}
              />
              {t(
                'I verified the provider outcome, request ownership and billing evidence.'
              )}
            </label>
            <Button
              type='submit'
              disabled={
                busy || !confirmed || detail.locked_until > Date.now() / 1000
              }
            >
              {t('Preview reviewed action')}
            </Button>
          </form>
        </Form>
      )}
      {!!detail.audits?.length && (
        <details>
          <summary>{t('Review history')}</summary>
          <ul className='flex flex-col gap-2'>
            {detail.audits.map((audit) => (
              <li key={audit.operation_id} className='text-sm break-words'>
                {audit.actor_id}: {audit.reason || audit.action} ·{' '}
                {audit.before_quota} → {audit.after_quota}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}

export function TaskReviewButton({ id }: { id: number }) {
  const { t } = useTranslation()
  const root = useAuthStore((s) => s.auth.user?.role === 100)
  const [open, setOpen] = useState(false)
  const client = useQueryClient()
  const detail = useQuery({
    queryKey: ['plugin-review', id],
    queryFn: () => reviewGet<Detail>(`/tasks/${id}/review`),
    enabled: root && open,
    retry: false,
  })
  if (!root) return null
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button size='sm' variant='outline'>
          {t('Review task')}
        </Button>
      </DialogTrigger>
      <DialogContent className='max-h-[90dvh] overflow-y-auto'>
        <DialogHeader>
          <DialogTitle>{t('Review task')}</DialogTitle>
          <DialogDescription>
            {t('Review unknown submissions and pending settlements.')}
          </DialogDescription>
        </DialogHeader>
        <Button
          variant='outline'
          disabled={detail.isFetching}
          onClick={() => void detail.refetch()}
        >
          {t('Refresh')}
        </Button>
        {detail.isPending || detail.isFetching ? (
          <Skeleton className='h-24' />
        ) : detail.data ? (
          <ReviewForm
            key={detail.data.revision}
            detail={detail.data}
            done={() => {
              setOpen(false)
              void client.invalidateQueries({ queryKey: ['logs'] })
              void client.invalidateQueries({ queryKey: ['plugin-review'] })
              void client.invalidateQueries({
                queryKey: ['plugin-review-queue'],
              })
            }}
          />
        ) : (
          <p>{t('Review failed. Refresh the task before trying again.')}</p>
        )}
      </DialogContent>
    </Dialog>
  )
}

export function TaskReviewQueue() {
  const { t } = useTranslation()
  const root = useAuthStore((s) => s.auth.user?.role === 100)
  const [open, setOpen] = useState(false)
  const [after, setAfter] = useState(0)
  const queue = useQuery({
    queryKey: ['plugin-review-queue', after],
    queryFn: () => reviewGet<Detail[]>(`/reviews?after=${after}`),
    enabled: root && open,
    retry: false,
  })
  if (!root) return null
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        setOpen(value)
        if (!value) setAfter(0)
      }}
    >
      <DialogTrigger asChild>
        <Button size='sm' variant='outline'>
          {t('Task review queue')}
        </Button>
      </DialogTrigger>
      <DialogContent className='max-h-[90dvh] overflow-y-auto'>
        <DialogHeader>
          <DialogTitle>{t('Task review queue')}</DialogTitle>
          <DialogDescription>
            {t('Review unknown submissions and pending settlements.')}
          </DialogDescription>
        </DialogHeader>
        {queue.isPending ? (
          <Skeleton className='h-24' />
        ) : queue.isError ? (
          <p>{t('Review failed. Refresh the task before trying again.')}</p>
        ) : (
          <ul className='flex flex-col gap-3'>
            {queue.data?.map((task) => (
              <li
                key={task.id}
                className='flex items-center justify-between gap-2'
              >
                <span className='truncate'>{task.task_id}</span>
                <TaskReviewButton id={task.id} />
              </li>
            ))}
          </ul>
        )}
        <div className='flex justify-end gap-2'>
          <Button
            variant='outline'
            onClick={() => {
              setAfter(0)
              void queue.refetch()
            }}
          >
            {t('Refresh')}
          </Button>
          <Button
            disabled={!queue.data || queue.data.length < 50}
            onClick={() => setAfter(queue.data?.at(-1)?.id ?? 0)}
          >
            {t('Next')}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
