import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Plus, Trash2 } from 'lucide-react'
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
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { createRule, updateRule } from '../api'
import {
  metricOptionsForScope,
  RISK_OP_OPTIONS,
} from '../lib/shared-utils'
import type { RiskCondition, RiskRule } from '../types'

const EMPTY_RULE: RiskRule = {
  id: 0,
  name: '',
  description: '',
  enabled: false,
  scope: 'token',
  detector: 'distribution',
  match_mode: 'all',
  priority: 50,
  action: 'observe',
  auto_block: false,
  auto_recover: true,
  recover_mode: 'ttl',
  recover_after_seconds: 900,
  response_status_code: 429,
  response_message: '',
  score_weight: 20,
  conditions: [{ metric: 'distinct_ip_10m', op: '>=', value: 3 }],
  groups: [],
}

type Props = {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: RiskRule | null
  groupOptions: string[]
  onSaved: () => void
}

export function RiskRuleDialog({
  open,
  onOpenChange,
  rule,
  groupOptions,
  onSaved,
}: Props) {
  const { t } = useTranslation()
  const isEdit = rule !== null && rule.id > 0
  const [form, setForm] = useState<RiskRule>(EMPTY_RULE)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    if (open) {
      setForm(rule ? { ...EMPTY_RULE, ...rule } : EMPTY_RULE)
    }
  }, [open, rule])

  const update = (patch: Partial<RiskRule>) =>
    setForm((prev) => ({ ...prev, ...patch }))

  const updateCondition = (idx: number, patch: Partial<RiskCondition>) => {
    setForm((prev) => ({
      ...prev,
      conditions: prev.conditions.map((c, i) =>
        i === idx ? { ...c, ...patch } : c
      ),
    }))
  }

  const removeCondition = (idx: number) => {
    setForm((prev) => ({
      ...prev,
      conditions: prev.conditions.filter((_, i) => i !== idx),
    }))
  }

  const addCondition = () => {
    const opts = metricOptionsForScope(form.scope)
    setForm((prev) => ({
      ...prev,
      conditions: [
        ...prev.conditions,
        { metric: opts[0]?.value || 'distinct_ip_10m', op: '>=', value: 1 },
      ],
    }))
  }

  const handleScope = (next: RiskRule['scope']) => {
    const opts = metricOptionsForScope(next)
    const allowed = new Set(opts.map((o) => o.value))
    setForm((prev) => ({
      ...prev,
      scope: next,
      conditions: prev.conditions.map((c) =>
        allowed.has(c.metric) ? c : { ...c, metric: opts[0]?.value || c.metric }
      ),
    }))
  }

  const submit = async () => {
    if (!form.name.trim()) {
      toast.error(t('Please enter a rule name'))
      return
    }
    if (form.conditions.length === 0) {
      toast.error(t('Add at least one condition'))
      return
    }
    setSubmitting(true)
    try {
      // Backend stores conditions / groups as JSON-encoded strings; serialize
      // before sending. The form holds them as real arrays so the UI can map
      // over them, then we re-encode here on submit.
      const payload = {
        ...form,
        conditions: JSON.stringify(form.conditions),
        groups: JSON.stringify(form.groups),
      } as unknown as Partial<RiskRule>
      if (isEdit) {
        await updateRule(form.id, payload)
        toast.success(t('Rule updated'))
      } else {
        await createRule(payload)
        toast.success(t('Rule created'))
      }
      onSaved()
      onOpenChange(false)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    } finally {
      setSubmitting(false)
    }
  }

  const metricOpts = metricOptionsForScope(form.scope)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='sm:max-w-2xl'>
        <DialogHeader>
          <DialogTitle>
            {isEdit ? t('Edit risk rule') : t('Create risk rule')}
          </DialogTitle>
          <DialogDescription>
            {t(
              'Rules trigger a decision when all (or any) conditions match for the configured scope.'
            )}
          </DialogDescription>
        </DialogHeader>

        <div className='max-h-[65vh] space-y-4 overflow-y-auto pr-1'>
          <div className='grid gap-3 sm:grid-cols-2'>
            <div className='space-y-1.5'>
              <Label>{t('Rule name')}</Label>
              <Input
                value={form.name}
                onChange={(e) => update({ name: e.target.value })}
                maxLength={120}
              />
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Priority')}</Label>
              <Input
                type='number'
                value={form.priority}
                onChange={(e) =>
                  update({ priority: Number(e.target.value) || 0 })
                }
              />
            </div>
            <div className='space-y-1.5 sm:col-span-2'>
              <Label>{t('Description')}</Label>
              <Textarea
                value={form.description}
                onChange={(e) => update({ description: e.target.value })}
                rows={2}
              />
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Scope')}</Label>
              <Select
                value={form.scope}
                onValueChange={(v) => handleScope(v as RiskRule['scope'])}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='token'>{t('Token')}</SelectItem>
                  <SelectItem value='user'>{t('User')}</SelectItem>
                  <SelectItem value='ip'>{t('IP')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Match mode')}</Label>
              <Select
                value={form.match_mode}
                onValueChange={(v) =>
                  update({ match_mode: v as RiskRule['match_mode'] })
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('Match all')}</SelectItem>
                  <SelectItem value='any'>{t('Match any')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Action')}</Label>
              <Select
                value={form.action}
                onValueChange={(v) => update({ action: v as RiskRule['action'] })}
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='observe'>{t('Observe')}</SelectItem>
                  <SelectItem value='block'>{t('Block')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1.5'>
              <Label>{t('Score weight')}</Label>
              <Input
                type='number'
                value={form.score_weight}
                onChange={(e) =>
                  update({ score_weight: Number(e.target.value) || 0 })
                }
              />
            </div>
          </div>

          <div className='space-y-2'>
            <div className='flex items-center justify-between'>
              <Label>{t('Conditions')}</Label>
              <Button
                type='button'
                size='sm'
                variant='outline'
                onClick={addCondition}
              >
                <Plus className='mr-1.5 h-3.5 w-3.5' />
                {t('Add condition')}
              </Button>
            </div>
            {form.conditions.map((c, i) => (
              <div
                key={i}
                className='bg-muted/40 flex flex-wrap items-end gap-2 rounded-lg border p-2'
              >
                <div className='flex-1 min-w-[150px]'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Metric')}
                  </Label>
                  <Select
                    value={c.metric}
                    onValueChange={(v) => updateCondition(i, { metric: v })}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {metricOpts.map((o) => (
                        <SelectItem key={o.value} value={o.value}>
                          {t(o.label)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='w-24'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Op')}
                  </Label>
                  <Select
                    value={c.op}
                    onValueChange={(v) => updateCondition(i, { op: v })}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {RISK_OP_OPTIONS.map((op) => (
                        <SelectItem key={op} value={op}>
                          {op}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className='w-28'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Value')}
                  </Label>
                  <Input
                    type='number'
                    value={c.value}
                    onChange={(e) =>
                      updateCondition(i, {
                        value: Number(e.target.value) || 0,
                      })
                    }
                  />
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='icon'
                  onClick={() => removeCondition(i)}
                  className='text-muted-foreground hover:text-destructive'
                >
                  <Trash2 className='h-4 w-4' />
                </Button>
              </div>
            ))}
          </div>

          {groupOptions.length > 0 && (
            <div className='space-y-1.5'>
              <Label>{t('Apply to groups')}</Label>
              <div className='flex flex-wrap gap-2 rounded-lg border p-2'>
                {groupOptions.map((g) => {
                  const checked = form.groups.includes(g)
                  return (
                    <label
                      key={g}
                      className='flex cursor-pointer items-center gap-1.5 text-sm'
                    >
                      <Checkbox
                        checked={checked}
                        onCheckedChange={(v) => {
                          setForm((prev) => ({
                            ...prev,
                            groups: v
                              ? [...prev.groups, g]
                              : prev.groups.filter((x) => x !== g),
                          }))
                        }}
                      />
                      {g}
                    </label>
                  )
                })}
              </div>
              <p className='text-muted-foreground text-xs'>
                {t('If empty, the rule applies to all groups.')}
              </p>
            </div>
          )}

          <div className='space-y-3 rounded-lg border p-3'>
            <div className='flex items-center justify-between'>
              <Label>{t('Enabled')}</Label>
              <Switch
                checked={form.enabled}
                onCheckedChange={(v) => update({ enabled: v })}
              />
            </div>
            <div className='flex items-center justify-between'>
              <Label>{t('Auto block')}</Label>
              <Switch
                checked={form.auto_block}
                onCheckedChange={(v) => update({ auto_block: v })}
              />
            </div>
            <div className='flex items-center justify-between'>
              <Label>{t('Auto recover')}</Label>
              <Switch
                checked={form.auto_recover}
                onCheckedChange={(v) => update({ auto_recover: v })}
              />
            </div>
            {form.auto_recover && (
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-1.5'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Recover mode')}
                  </Label>
                  <Select
                    value={form.recover_mode}
                    onValueChange={(v) =>
                      update({ recover_mode: v as RiskRule['recover_mode'] })
                    }
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value='ttl'>{t('TTL')}</SelectItem>
                      <SelectItem value='manual'>{t('Manual')}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className='space-y-1.5'>
                  <Label className='text-muted-foreground text-xs'>
                    {t('Recover after (seconds)')}
                  </Label>
                  <Input
                    type='number'
                    value={form.recover_after_seconds}
                    onChange={(e) =>
                      update({
                        recover_after_seconds: Number(e.target.value) || 0,
                      })
                    }
                  />
                </div>
              </div>
            )}
            <div className='grid gap-3 sm:grid-cols-2'>
              <div className='space-y-1.5'>
                <Label className='text-muted-foreground text-xs'>
                  {t('Response status code')}
                </Label>
                <Input
                  type='number'
                  value={form.response_status_code}
                  onChange={(e) =>
                    update({
                      response_status_code: Number(e.target.value) || 429,
                    })
                  }
                />
              </div>
              <div className='space-y-1.5'>
                <Label className='text-muted-foreground text-xs'>
                  {t('Response message')}
                </Label>
                <Input
                  value={form.response_message}
                  onChange={(e) =>
                    update({ response_message: e.target.value })
                  }
                />
              </div>
            </div>
          </div>
        </div>

        <DialogFooter>
          <Button
            variant='outline'
            onClick={() => onOpenChange(false)}
            disabled={submitting}
          >
            {t('Cancel')}
          </Button>
          <Button onClick={submit} disabled={submitting}>
            {t('Submit')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
