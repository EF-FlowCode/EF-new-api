import { useEffect, useMemo, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { StatusBadge } from '@/components/status-badge'
import { confirmChannelProtocols, probeChannelProtocols } from '../../api'
import {
  channelsQueryKeys,
  defaultProtocolSelection,
  describeProtocolChanges,
  normalizeUpstreamProtocols,
  parseChannelOtherSettings,
  supportsProtocolRouting,
  UPSTREAM_PROTOCOL_PATHS,
  UPSTREAM_PROTOCOLS,
  type ProtocolRouteOutcome,
} from '../../lib'
import type {
  UpstreamProtocol,
  UpstreamProtocolProbeItem,
  UpstreamProtocolProbeResult,
} from '../../types'
import { useChannels } from '../channels-provider'

type ChannelProtocolDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

const VERDICT_BADGE: Record<
  UpstreamProtocolProbeItem['verdict'],
  { label: string; variant: 'success' | 'danger' | 'warning' }
> = {
  supported: { label: 'Supported', variant: 'success' },
  unsupported: { label: 'Unsupported', variant: 'danger' },
  unknown: { label: 'Inconclusive', variant: 'warning' },
}

const REASON_LABELS: Record<string, string> = {
  answer_mismatch:
    'Wrong answer to the random question; the upstream may return canned replies',
  client_restricted:
    'Upstream only accepts a specific client (e.g. Claude Code); use the Claude Code native test',
  model_not_found: 'Upstream reports the model does not exist',
  endpoint_missing: 'Upstream has no such endpoint',
  format_error: 'Upstream rejected the request format',
  stream_failed: 'Non-stream works but stream failed',
  inconclusive: 'Could not determine; see the attempt details',
}

const OUTCOME_LABELS: Record<ProtocolRouteOutcome | 'legacy', string> = {
  legacy: 'legacy behavior',
  native: 'native passthrough',
  chat_fallback: 'Chat fallback',
  messages_fallback: 'converted to Messages',
  rejected: 'not served',
}

export function ChannelProtocolDialog({
  open,
  onOpenChange,
}: ChannelProtocolDialogProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { currentRow } = useChannels()
  const settings = useMemo(
    () => parseChannelOtherSettings(currentRow?.settings),
    [currentRow?.settings]
  )
  const saved = useMemo(
    () => normalizeUpstreamProtocols(settings.upstream_protocols),
    [settings.upstream_protocols]
  )
  const passThrough = useMemo(() => {
    try {
      return Boolean(
        JSON.parse(currentRow?.setting || '{}').pass_through_body_enabled
      )
    } catch {
      return false
    }
  }, [currentRow?.setting])

  const [probe, setProbe] = useState<UpstreamProtocolProbeResult | undefined>()
  const [selected, setSelected] = useState<UpstreamProtocol[]>([])
  const [model, setModel] = useState('')
  const [probing, setProbing] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!open) return
    setProbe(settings.upstream_protocol_probe)
    setSelected(saved)
    setModel(currentRow?.test_model?.trim() || '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, currentRow?.id])

  if (!currentRow) return null
  const routable = supportsProtocolRouting(currentRow.type)
  const changes = describeProtocolChanges(
    saved,
    selected,
    passThrough,
    currentRow.type
  )

  const runProbe = async () => {
    setProbing(true)
    try {
      const res = await probeChannelProtocols(
        currentRow.id,
        model.trim() || undefined
      )
      if (!res.success || !res.data) {
        toast.error(res.message || t('Probe failed'))
        return
      }
      setProbe(res.data)
      // Pre-check the probe's "supported" protocols; nothing is saved yet.
      setSelected(defaultProtocolSelection(res.data))
      queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Probe failed'))
    } finally {
      setProbing(false)
    }
  }

  const save = async () => {
    setSaving(true)
    try {
      const res = await confirmChannelProtocols(currentRow.id, selected)
      if (!res.success) {
        toast.error(res.message || t('Save failed'))
        return
      }
      toast.success(t('Upstream protocols saved'))
      queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
      onOpenChange(false)
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Save failed'))
    } finally {
      setSaving(false)
    }
  }

  const toggle = (protocol: UpstreamProtocol, checked: boolean) => {
    setSelected((prev) =>
      normalizeUpstreamProtocols(
        checked ? [...prev, protocol] : prev.filter((p) => p !== protocol)
      )
    )
  }

  const itemFor = (protocol: UpstreamProtocol) =>
    probe?.items.find((item) => item.protocol === protocol)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='max-h-[90vh] overflow-hidden sm:max-w-3xl'>
        <DialogHeader>
          <DialogTitle>{t('Upstream Protocols')}</DialogTitle>
          <DialogDescription>
            {t(
              'Probe results are for reference only. Routing changes only after you confirm and save.'
            )}{' '}
            <strong>{currentRow.name}</strong>
          </DialogDescription>
        </DialogHeader>

        {!routable ? (
          <p className='text-muted-foreground py-4 text-sm'>
            {t('This channel type does not support protocol routing.')}
          </p>
        ) : (
          <div className='max-h-[70vh] space-y-4 overflow-y-auto py-2 pr-1'>
            <div className='flex flex-col gap-2 sm:flex-row sm:items-end'>
              <div className='grid flex-1 gap-2'>
                <Label htmlFor='protocol-probe-model'>{t('Probe model')}</Label>
                <Input
                  id='protocol-probe-model'
                  value={model}
                  placeholder={t('Default: test model or first model')}
                  onChange={(e) => setModel(e.target.value)}
                />
              </div>
              <Button onClick={runProbe} disabled={probing || saving}>
                {probing && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
                {t('Run probe')}
              </Button>
            </div>
            {probe && (
              <p className='text-muted-foreground text-xs'>
                {t('Last probe')}:{' '}
                {new Date(probe.probed_at * 1000).toLocaleString()} ·{' '}
                {t('Upstream model')}: {probe.model}
              </p>
            )}

            <div className='divide-y rounded-md border'>
              {UPSTREAM_PROTOCOLS.map((protocol) => {
                const item = itemFor(protocol)
                const badge = item ? VERDICT_BADGE[item.verdict] : undefined
                return (
                  <div key={protocol} className='flex gap-3 p-3'>
                    <Checkbox
                      id={`protocol-${protocol}`}
                      checked={selected.includes(protocol)}
                      onCheckedChange={(v) => toggle(protocol, v === true)}
                      aria-label={t('Enable {{protocol}}', { protocol })}
                    />
                    <div className='flex-1 space-y-1'>
                      <div className='flex flex-wrap items-center gap-2'>
                        <Label
                          htmlFor={`protocol-${protocol}`}
                          className='font-medium'
                        >
                          {protocol}
                        </Label>
                        <span className='text-muted-foreground font-mono text-xs'>
                          {UPSTREAM_PROTOCOL_PATHS[protocol]}
                        </span>
                        {badge ? (
                          <StatusBadge
                            label={t(badge.label)}
                            variant={badge.variant}
                            size='sm'
                            copyable={false}
                          />
                        ) : (
                          <StatusBadge
                            label={t('Not probed')}
                            variant='neutral'
                            size='sm'
                            copyable={false}
                          />
                        )}
                        {item && item.traffic_count > 0 && (
                          <span className='text-muted-foreground text-xs'>
                            {t('Native successes in last 7 days: {{count}}', {
                              count: item.traffic_count,
                            })}
                          </span>
                        )}
                      </div>
                      {item?.reason && (
                        <p className='text-muted-foreground text-xs'>
                          {t(REASON_LABELS[item.reason] ?? item.reason)}
                        </p>
                      )}
                      {item?.attempts?.map((attempt) => (
                        <p
                          key={String(attempt.stream)}
                          className='text-muted-foreground text-xs break-all'
                        >
                          {attempt.stream ? t('Stream') : t('Non-stream')}:{' '}
                          {attempt.status_code || '-'} · {attempt.duration_ms}ms
                          {attempt.ok
                            ? attempt.answer_ok
                              ? ` · ${t('answer correct')}`
                              : ` · ${t('answer wrong')}`
                            : attempt.error
                              ? ` · ${attempt.error}`
                              : ''}
                        </p>
                      ))}
                    </div>
                  </div>
                )
              })}
            </div>

            <div className='rounded-md border p-3 text-sm'>
              <p className='font-medium'>
                {t('Current setting')}:{' '}
                {saved.length
                  ? saved.join(', ')
                  : t('Not declared (legacy behavior)')}
              </p>
              {passThrough && (
                <p className='text-muted-foreground mt-1 text-xs'>
                  {t(
                    'Request body pass-through is on, so this channel never falls back to Chat.'
                  )}
                </p>
              )}
              {changes.length > 0 ? (
                <ul className='mt-2 space-y-1 text-xs'>
                  {changes.map((change) => (
                    <li key={change.protocol}>
                      {t('{{path}} requests: {{before}} → {{after}}', {
                        path: UPSTREAM_PROTOCOL_PATHS[change.protocol],
                        before: t(OUTCOME_LABELS[change.before]),
                        after: t(OUTCOME_LABELS[change.after]),
                      })}
                    </li>
                  ))}
                </ul>
              ) : (
                <p className='text-muted-foreground mt-2 text-xs'>
                  {t('No routing change.')}
                </p>
              )}
            </div>
          </div>
        )}

        <DialogFooter>
          <Button variant='outline' onClick={() => onOpenChange(false)}>
            {t('Cancel')}
          </Button>
          {routable && (
            <Button
              onClick={save}
              disabled={saving || probing || changes.length === 0}
            >
              {saving && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {t('Confirm and save')}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
