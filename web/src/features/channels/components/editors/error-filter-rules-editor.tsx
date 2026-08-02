import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  ChevronDown,
  ChevronUp,
  Filter,
  History,
  Plus,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { TagInput } from '@/components/tag-input'
import { cn } from '@/lib/utils'
import { api } from '@/lib/api'
import {
  createEmptyErrorFilterRule,
  ERROR_FILTER_ACTIONS,
  normalizeErrorFilterRule,
  type ErrorFilterAction,
  type ErrorFilterRule,
} from '../../lib/channel-extra-rules'

type Props = {
  value: ErrorFilterRule[] | undefined
  onChange: (next: ErrorFilterRule[]) => void
  channelId?: number
}

const ACTION_LABELS: Record<ErrorFilterAction, string> = {
  retry: 'Switch channel and retry',
  rewrite: 'Rewrite message (passthrough status code)',
  replace: 'Intercept and replace response',
}

const ACTION_BADGE_LABELS: Record<ErrorFilterAction, string> = {
  retry: 'Retry',
  rewrite: 'Rewrite',
  replace: 'Replace',
}

const ACTION_BADGE_VARIANTS: Record<ErrorFilterAction, string> = {
  retry: 'border-blue-500/40 bg-blue-500/10 text-blue-600 dark:text-blue-400',
  rewrite:
    'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400',
  replace:
    'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400',
}

const formatTimestamp = (ts: number | undefined): string => {
  if (!ts) return ''
  const date = new Date(ts * 1000)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString()
}

// ============================================================================
// Recent errors picker dialog
// ============================================================================

type RecentError = {
  id: number
  createdAt: number
  content: string
  modelName: string
  statusCode: number | null
  errorCode: string
  errorType: string
}

const parseErrorLog = (log: Record<string, unknown>): RecentError => {
  let other: Record<string, unknown> = {}
  try {
    if (typeof log.other === 'string' && log.other) {
      other = JSON.parse(log.other) as Record<string, unknown>
    }
  } catch {
    other = {}
  }
  const statusRaw = Number.parseInt(String(other.status_code ?? ''), 10)
  return {
    id: Number(log.id) || 0,
    createdAt: Number(log.created_at) || 0,
    content: String(log.content || ''),
    modelName: String(log.model_name || ''),
    statusCode: Number.isFinite(statusRaw) ? statusRaw : null,
    errorCode: other.error_code ? String(other.error_code) : '',
    errorType: other.error_type ? String(other.error_type) : '',
  }
}

const dedupeErrors = (logs: RecentError[]): RecentError[] => {
  const seen = new Map<string, RecentError>()
  for (const log of logs) {
    const key = `${log.statusCode || 0}|${log.errorCode}|${log.content}`
    if (!seen.has(key)) seen.set(key, log)
  }
  return Array.from(seen.values())
}

type RecentErrorsDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  channelId: number
  onApply: (data: {
    status_codes: number[]
    error_codes: string[]
    messages: string[]
  }) => void
}

function RecentErrorsDialog({
  open,
  onOpenChange,
  channelId,
  onApply,
}: RecentErrorsDialogProps) {
  const { t } = useTranslation()
  const [loading, setLoading] = useState(false)
  const [errors, setErrors] = useState<RecentError[]>([])
  const [selected, setSelected] = useState<Set<number>>(new Set())

  const fetchErrors = async () => {
    if (!channelId) return
    setLoading(true)
    try {
      const res = await api.get(
        `/api/log/?type=5&channel=${channelId}&p=1&page_size=50`
      )
      const data = res.data
      if (data?.success) {
        const items: Record<string, unknown>[] = data.data?.items || []
        setErrors(dedupeErrors(items.map(parseErrorLog)))
      } else {
        setErrors([])
      }
    } catch {
      setErrors([])
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    if (open) {
      setSelected(new Set())
      fetchErrors()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, channelId])

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleApply = () => {
    const picked = errors.filter((e) => selected.has(e.id))
    if (picked.length === 0) {
      onOpenChange(false)
      return
    }
    onApply({
      status_codes: Array.from(
        new Set(
          picked.map((e) => e.statusCode).filter((c): c is number => c != null)
        )
      ),
      error_codes: Array.from(
        new Set(picked.map((e) => e.errorCode).filter(Boolean))
      ),
      messages: Array.from(
        new Set(picked.map((e) => e.content).filter(Boolean))
      ),
    })
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle className='flex items-center gap-2'>
            <History className='h-4 w-4' />
            {t('Pick from recent errors')}
          </DialogTitle>
          <DialogDescription>
            {t('Showing the last 50 errors for this channel (deduplicated by content)')}
          </DialogDescription>
        </DialogHeader>

        <div className='flex items-center justify-between'>
          <div className='text-muted-foreground text-xs'>
            {t('Selected {{n}}', { n: selected.size })}
          </div>
          <Button
            type='button'
            size='sm'
            variant='ghost'
            onClick={fetchErrors}
            disabled={loading}
          >
            <RefreshCw
              className={cn('mr-1.5 h-3.5 w-3.5', loading && 'animate-spin')}
            />
            {t('Refresh')}
          </Button>
        </div>

        <div className='max-h-[60vh] space-y-2 overflow-y-auto'>
          {errors.length === 0 && !loading ? (
            <div className='text-muted-foreground py-12 text-center text-sm'>
              {t('No error records')}
            </div>
          ) : (
            errors.map((err) => {
              const checked = selected.has(err.id)
              return (
                <button
                  type='button'
                  key={err.id}
                  onClick={() => toggle(err.id)}
                  className={cn(
                    'w-full rounded-lg border p-3 text-left transition-colors',
                    checked
                      ? 'border-primary bg-primary/5'
                      : 'hover:bg-accent/40'
                  )}
                >
                  <div className='flex items-start gap-3'>
                    <Checkbox
                      checked={checked}
                      onCheckedChange={() => toggle(err.id)}
                      onClick={(e) => e.stopPropagation()}
                      className='mt-1'
                    />
                    <div className='min-w-0 flex-1'>
                      <div className='mb-1 flex flex-wrap items-center gap-1.5'>
                        {err.statusCode && (
                          <Badge variant='outline' className='font-normal'>
                            {err.statusCode}
                          </Badge>
                        )}
                        {err.errorCode && (
                          <Badge variant='outline' className='font-normal'>
                            {err.errorCode}
                          </Badge>
                        )}
                        {err.modelName && (
                          <Badge variant='secondary' className='font-normal'>
                            {err.modelName}
                          </Badge>
                        )}
                        <span className='text-muted-foreground text-xs'>
                          {formatTimestamp(err.createdAt)}
                        </span>
                      </div>
                      <p className='line-clamp-2 text-sm break-words'>
                        {err.content || (
                          <span className='text-muted-foreground'>
                            {t('(empty message)')}
                          </span>
                        )}
                      </p>
                    </div>
                  </div>
                </button>
              )
            })
          )}
        </div>

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            disabled={selected.size === 0}
            onClick={handleApply}
          >
            {t('Apply selected')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// ============================================================================
// Single rule
// ============================================================================

type RuleItemProps = {
  rule: ErrorFilterRule
  index: number
  channelId?: number
  onUpdate: (index: number, patch: Partial<ErrorFilterRule>) => void
  onRemove: (index: number) => void
}

function RuleSummary({ rule }: { rule: ErrorFilterRule }) {
  const { t } = useTranslation()
  const conditions: string[] = []
  if (rule.status_codes.length > 0) {
    conditions.push(rule.status_codes.join(' / '))
  }
  if (rule.message_contains.length > 0) {
    const head = rule.message_contains.slice(0, 2).join('", "')
    conditions.push(
      `"${head}${rule.message_contains.length > 2 ? '"…' : '"'}`
    )
  }
  if (rule.error_codes.length > 0) {
    conditions.push(
      rule.error_codes.slice(0, 2).join(', ') +
        (rule.error_codes.length > 2 ? '…' : '')
    )
  }

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Badge
        variant='outline'
        className={cn('font-normal', ACTION_BADGE_VARIANTS[rule.action])}
      >
        {t(ACTION_BADGE_LABELS[rule.action])}
      </Badge>
      <span className='text-muted-foreground text-xs'>
        {conditions.length > 0
          ? conditions.join(' · ')
          : t('No matchers (matches every error)')}
      </span>
    </div>
  )
}

function RuleItem({
  rule,
  index,
  channelId,
  onUpdate,
  onRemove,
}: RuleItemProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(true)
  const [recentOpen, setRecentOpen] = useState(false)

  const hasCondition =
    rule.status_codes.length > 0 ||
    rule.message_contains.length > 0 ||
    rule.error_codes.length > 0

  const handleApplyRecent = ({
    status_codes,
    error_codes,
    messages,
  }: {
    status_codes: number[]
    error_codes: string[]
    messages: string[]
  }) => {
    onUpdate(index, {
      status_codes: Array.from(
        new Set([...rule.status_codes, ...status_codes])
      ),
      error_codes: Array.from(new Set([...rule.error_codes, ...error_codes])),
      message_contains: Array.from(
        new Set([...rule.message_contains, ...messages])
      ),
    })
  }

  return (
    <div className='bg-card overflow-hidden rounded-lg border'>
      <button
        type='button'
        className={cn(
          'flex w-full select-none items-center justify-between px-3 py-2.5',
          expanded && 'border-b'
        )}
        onClick={() => setExpanded((v) => !v)}
      >
        <div className='flex min-w-0 items-center gap-2.5'>
          <span className='bg-primary text-primary-foreground flex h-5 w-5 shrink-0 items-center justify-center rounded-full text-[11px] font-medium'>
            {index + 1}
          </span>
          {expanded ? (
            <span className='text-sm font-medium'>
              {t('Rule {{n}}', { n: index + 1 })}
            </span>
          ) : (
            <RuleSummary rule={rule} />
          )}
        </div>
        <div
          className='flex shrink-0 items-center gap-1'
          onClick={(e) => e.stopPropagation()}
        >
          <Button
            type='button'
            size='icon'
            variant='ghost'
            className='text-muted-foreground hover:text-destructive h-7 w-7'
            onClick={() => onRemove(index)}
          >
            <Trash2 className='h-3.5 w-3.5' />
          </Button>
          <Button
            type='button'
            size='icon'
            variant='ghost'
            className='text-muted-foreground h-7 w-7'
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? (
              <ChevronUp className='h-3.5 w-3.5' />
            ) : (
              <ChevronDown className='h-3.5 w-3.5' />
            )}
          </Button>
        </div>
      </button>

      {expanded && (
        <div className='space-y-4 p-3'>
          <div className='space-y-3'>
            <div className='flex flex-wrap items-center justify-between gap-2'>
              <div className='space-x-1.5'>
                <span className='text-sm font-medium'>{t('Matchers')}</span>
                <span className='text-muted-foreground text-xs'>
                  {t('(AND across types, OR within a type)')}
                </span>
              </div>
              {channelId ? (
                <Button
                  type='button'
                  size='sm'
                  variant='ghost'
                  onClick={() => setRecentOpen(true)}
                  className='h-7 px-2 text-xs'
                >
                  <History className='mr-1.5 h-3 w-3' />
                  {t('Pick from error history')}
                </Button>
              ) : null}
            </div>

            <div className='space-y-1.5'>
              <Label className='text-muted-foreground text-xs font-normal'>
                {t('HTTP status codes')}
              </Label>
              <TagInput
                value={rule.status_codes.map(String)}
                onChange={(vals) =>
                  onUpdate(index, {
                    status_codes: Array.from(
                      new Set(
                        vals
                          .map((v) => Number.parseInt(v.trim(), 10))
                          .filter((v) => Number.isInteger(v) && v >= 100 && v <= 599)
                      )
                    ),
                  })
                }
                placeholder={t('Enter and press enter, e.g. 429')}
              />
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label className='text-muted-foreground text-xs font-normal'>
                  {t('Error codes')}
                </Label>
                <TagInput
                  value={rule.error_codes}
                  onChange={(vals) =>
                    onUpdate(index, {
                      error_codes: Array.from(
                        new Set(
                          vals.map((v) => v.trim()).filter(Boolean)
                        )
                      ),
                    })
                  }
                  placeholder={t('e.g. rate_limit_exceeded')}
                />
              </div>
              <div className='space-y-1.5'>
                <Label className='text-muted-foreground text-xs font-normal'>
                  {t('Message contains')}
                </Label>
                <TagInput
                  value={rule.message_contains}
                  onChange={(vals) =>
                    onUpdate(index, {
                      message_contains: Array.from(
                        new Set(
                          vals.map((v) => v.trim()).filter(Boolean)
                        )
                      ),
                    })
                  }
                  placeholder={t('e.g. rate limit')}
                />
              </div>
            </div>
            {!hasCondition && (
              <div className='rounded-md border border-amber-500/40 bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-700 dark:text-amber-300'>
                {t('⚠ No matcher set: this rule will match every upstream error')}
              </div>
            )}
          </div>

          <div className='space-y-1.5'>
            <Label className='text-sm font-medium'>{t('Action')}</Label>
            <Select
              value={rule.action}
              onValueChange={(v) =>
                onUpdate(index, {
                  action: (ERROR_FILTER_ACTIONS.includes(
                    v as ErrorFilterAction
                  )
                    ? v
                    : 'retry') as ErrorFilterAction,
                })
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ERROR_FILTER_ACTIONS.map((action) => (
                  <SelectItem key={action} value={action}>
                    {t(ACTION_LABELS[action])}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {rule.action === 'rewrite' && (
            <div className='space-y-1.5'>
              <Label className='text-muted-foreground text-xs font-normal'>
                {t('Rewrite message')}{' '}
                <span className='text-muted-foreground/60'>
                  {t('(status code passthrough)')}
                </span>
              </Label>
              <Input
                value={rule.rewrite_message}
                placeholder={t('Enter rewritten error message')}
                onChange={(e) =>
                  onUpdate(index, { rewrite_message: e.target.value })
                }
              />
              {!rule.rewrite_message && (
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Empty: keep original message; only suppress retry / auto-disable'
                  )}
                </p>
              )}
            </div>
          )}

          {rule.action === 'replace' && (
            <div className='space-y-1.5'>
              <Label className='text-muted-foreground text-xs font-normal'>
                {t('Replace response')}
              </Label>
              <div className='grid gap-2 sm:grid-cols-12'>
                <div className='sm:col-span-3'>
                  <Input
                    type='number'
                    min={100}
                    max={599}
                    value={rule.replace_status_code}
                    onChange={(e) => {
                      const n = Number.parseInt(e.target.value, 10)
                      onUpdate(index, {
                        replace_status_code:
                          Number.isInteger(n) && n >= 100 ? n : 200,
                      })
                    }}
                  />
                </div>
                <div className='sm:col-span-9'>
                  <Input
                    value={rule.replace_message}
                    placeholder={t('Replacement error message')}
                    onChange={(e) =>
                      onUpdate(index, { replace_message: e.target.value })
                    }
                  />
                </div>
              </div>
            </div>
          )}

          {rule.action === 'retry' && (
            <div className='bg-primary/5 text-primary rounded-md px-3 py-2 text-xs'>
              {t(
                'On match, force-switch to the next channel and ignore the original retry policy'
              )}
            </div>
          )}
        </div>
      )}

      {channelId ? (
        <RecentErrorsDialog
          open={recentOpen}
          onOpenChange={setRecentOpen}
          channelId={channelId}
          onApply={handleApplyRecent}
        />
      ) : null}
    </div>
  )
}

// ============================================================================
// Main editor
// ============================================================================

export function ErrorFilterRulesEditor({ value, onChange, channelId }: Props) {
  const { t } = useTranslation()

  const rules = useMemo<ErrorFilterRule[]>(
    () => (Array.isArray(value) ? value.map(normalizeErrorFilterRule) : []),
    [value]
  )

  const emit = (next: ErrorFilterRule[]) => {
    onChange(next.map(normalizeErrorFilterRule))
  }

  const updateRule = (index: number, patch: Partial<ErrorFilterRule>) =>
    emit(rules.map((r, i) => (i === index ? { ...r, ...patch } : r)))

  const removeRule = (index: number) => emit(rules.filter((_, i) => i !== index))

  const addRule = () => emit([...rules, createEmptyErrorFilterRule()])

  return (
    <div className='space-y-3'>
      <div className='flex items-start justify-between gap-3'>
        <div className='flex items-start gap-2.5'>
          <span className='bg-primary/10 text-primary mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg'>
            <Filter className='h-3.5 w-3.5' />
          </span>
          <div>
            <div className='text-sm font-semibold'>
              {t('Upstream error filter')}
            </div>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Rules are evaluated in order; the first match wins. Supports retry, rewrite, or replace.'
              )}
            </p>
          </div>
        </div>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={addRule}
          className='shrink-0'
        >
          <Plus className='mr-1.5 h-3.5 w-3.5' />
          {t('Add rule')}
        </Button>
      </div>

      {rules.length === 0 ? (
        <div className='bg-muted/40 text-muted-foreground rounded-lg border border-dashed px-4 py-6 text-center text-sm'>
          {t('No rules')}
        </div>
      ) : (
        <div className='space-y-2'>
          {rules.map((rule, index) => (
            <RuleItem
              key={index}
              rule={rule}
              index={index}
              channelId={channelId}
              onUpdate={updateRule}
              onRemove={removeRule}
            />
          ))}
        </div>
      )}
    </div>
  )
}
