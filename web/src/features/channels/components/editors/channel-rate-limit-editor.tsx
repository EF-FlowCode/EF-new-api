import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, Clock } from 'lucide-react'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Switch } from '@/components/ui/switch'
import { cn } from '@/lib/utils'
import {
  DEFAULT_CHANNEL_RATE_LIMIT,
  normalizeChannelRateLimit,
  type ChannelRateLimit,
} from '../../lib/channel-extra-rules'
import { useRedisStatus } from '../../hooks/use-redis-status'

type Props = {
  value: ChannelRateLimit | undefined
  onChange: (next: ChannelRateLimit) => void
}

const ON_LIMIT_OPTIONS = [
  {
    value: 'skip' as const,
    titleKey: 'Skip',
    descKey:
      'When this channel is saturated, automatically route to other channels in the same group',
  },
  {
    value: 'queue' as const,
    titleKey: 'Queue and wait',
    descKey:
      'Wait serially at the gateway; once the max wait time or queue depth is exceeded, fall back to skip',
  },
  {
    value: 'reject' as const,
    titleKey: 'Reject directly (return 429)',
    descKey: 'Return 429 immediately without trying other channels',
  },
]

export function ChannelRateLimitEditor({ value, onChange }: Props) {
  const { t } = useTranslation()
  const { redisEnabled } = useRedisStatus()

  const v = useMemo(
    () => normalizeChannelRateLimit(value ?? DEFAULT_CHANNEL_RATE_LIMIT),
    [value]
  )

  const update = (patch: Partial<ChannelRateLimit>) => {
    onChange(normalizeChannelRateLimit({ ...v, ...patch }))
  }

  const disabled = !v.enabled

  return (
    <div className='space-y-4'>
      <div className='flex items-start justify-between gap-3'>
        <div className='flex items-start gap-2.5'>
          <span className='bg-primary/10 text-primary mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg'>
            <Clock className='h-3.5 w-3.5' />
          </span>
          <div>
            <div className='text-sm font-semibold'>{t('Channel rate limit')}</div>
            <p className='text-muted-foreground text-xs'>
              {t(
                "Limit this channel's requests per minute (RPM) and concurrency to avoid upstream throttling; when saturated, skip to other channels in the same group, queue serially, or reject directly"
              )}
            </p>
          </div>
        </div>
        <Switch
          checked={v.enabled}
          onCheckedChange={(checked) => update({ enabled: checked })}
        />
      </div>

      {!redisEnabled && (
        <Alert className='border-amber-500/50 bg-amber-500/10 text-amber-700 dark:text-amber-300 [&>svg]:text-amber-600 dark:[&>svg]:text-amber-400'>
          <AlertTriangle className='h-4 w-4' />
          <AlertDescription>
            {t(
              'Redis is not enabled: channel rate limiting requires Redis for accurate enforcement across replicas. Single-instance deployments still work, but each replica counts independently when running multiple instances.'
            )}
          </AlertDescription>
        </Alert>
      )}

      <div className={cn('grid gap-3 sm:grid-cols-2', disabled && 'opacity-60')}>
        <div className='space-y-1.5'>
          <Label className='text-muted-foreground text-xs font-normal'>
            {t('Requests per minute (RPM, 0 = unlimited)')}
          </Label>
          <Input
            type='number'
            min={0}
            value={v.rpm}
            disabled={disabled}
            onChange={(e) => {
              const n = Number(e.target.value)
              update({ rpm: Number.isFinite(n) ? Math.max(0, Math.floor(n)) : 0 })
            }}
          />
        </div>
        <div className='space-y-1.5'>
          <Label className='text-muted-foreground text-xs font-normal'>
            {t('Concurrency (in-flight requests, 0 = unlimited)')}
          </Label>
          <Input
            type='number'
            min={0}
            value={v.concurrency}
            disabled={disabled}
            onChange={(e) => {
              const n = Number(e.target.value)
              update({
                concurrency: Number.isFinite(n) ? Math.max(0, Math.floor(n)) : 0,
              })
            }}
          />
        </div>
      </div>

      <div className={cn('space-y-2', disabled && 'opacity-60')}>
        <Label className='text-muted-foreground text-xs font-normal'>
          {t('On-limit strategy')}
        </Label>
        <RadioGroup
          value={v.on_limit}
          onValueChange={(next) =>
            update({ on_limit: next as ChannelRateLimit['on_limit'] })
          }
          disabled={disabled}
          className='space-y-2'
        >
          {ON_LIMIT_OPTIONS.map(({ value: optValue, titleKey, descKey }) => (
            <Label
              key={optValue}
              htmlFor={`rl-${optValue}`}
              className={cn(
                'flex cursor-pointer items-start gap-3 rounded-lg border p-3 transition-colors',
                v.on_limit === optValue
                  ? 'border-primary bg-primary/5'
                  : 'hover:bg-accent/40',
                disabled && 'cursor-not-allowed opacity-60'
              )}
            >
              <RadioGroupItem
                id={`rl-${optValue}`}
                value={optValue}
                className='mt-0.5'
              />
              <div className='space-y-0.5'>
                <div className='text-sm font-medium'>{t(titleKey)}</div>
                <div className='text-muted-foreground text-xs'>
                  {t(descKey)}
                </div>
              </div>
            </Label>
          ))}
        </RadioGroup>
      </div>

      {v.on_limit === 'queue' && (
        <div
          className={cn(
            'grid gap-3 sm:grid-cols-2',
            disabled && 'opacity-60'
          )}
        >
          <div className='space-y-1.5'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Max wait (ms)')}
            </Label>
            <Input
              type='number'
              min={0}
              max={60000}
              step={100}
              value={v.queue_max_wait_ms}
              disabled={disabled}
              onChange={(e) => {
                const n = Number(e.target.value)
                update({
                  queue_max_wait_ms: Number.isFinite(n)
                    ? Math.max(0, Math.floor(n))
                    : 0,
                })
              }}
            />
          </div>
          <div className='space-y-1.5'>
            <Label className='text-muted-foreground text-xs font-normal'>
              {t('Queue depth (max queued requests)')}
            </Label>
            <Input
              type='number'
              min={0}
              max={10000}
              value={v.queue_depth}
              disabled={disabled}
              onChange={(e) => {
                const n = Number(e.target.value)
                update({
                  queue_depth: Number.isFinite(n)
                    ? Math.max(0, Math.floor(n))
                    : 0,
                })
              }}
            />
          </div>
        </div>
      )}
    </div>
  )
}
