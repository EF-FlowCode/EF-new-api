import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
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
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  debugModeration,
  deleteModerationRule,
  fetchModerationCategories,
  fetchModerationConfig,
  fetchModerationIncidents,
  fetchModerationOverview,
  fetchModerationQueueStats,
  fetchModerationRules,
  saveModerationRule,
  updateModerationConfig,
} from '../api'
import { DecisionBadge, OverviewCard } from '../lib/shared'
import { formatRiskTime } from '../lib/shared-utils'
import type {
  ModerationCategory,
  ModerationConfig,
  ModerationIncident,
  ModerationQueueStats,
  ModerationRule,
  Paginated,
  RiskOverview,
} from '../types'
import { PaginationFooter } from './distribution-tab'

const PAGE_SIZE = 10

const EMPTY_RULE: ModerationRule = {
  id: 0,
  name: '',
  enabled: true,
  category: '',
  threshold: 0.5,
  action: 'observe',
  groups: [],
  description: '',
  priority: 50,
}

export function ModerationTab() {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<RiskOverview>({})
  const [queueStats, setQueueStats] = useState<ModerationQueueStats>({})
  const [config, setConfig] = useState<ModerationConfig>({})
  const [categories, setCategories] = useState<ModerationCategory[]>([])
  const [rules, setRules] = useState<ModerationRule[]>([])
  const [savingConfig, setSavingConfig] = useState(false)

  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<ModerationRule>(EMPTY_RULE)
  const [savingRule, setSavingRule] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ModerationRule | null>(null)

  const [incidents, setIncidents] = useState<Paginated<ModerationIncident>>({
    items: [],
    page: 1,
    page_size: PAGE_SIZE,
    total: 0,
  })
  const [incidentsLoading, setIncidentsLoading] = useState(false)

  const [debugText, setDebugText] = useState('')
  const [debugImages, setDebugImages] = useState('')
  const [debugGroup, setDebugGroup] = useState('')
  const [debugRunning, setDebugRunning] = useState(false)
  const [debugResult, setDebugResult] = useState<unknown>(null)

  const reload = async () => {
    try {
      const [o, q, c, cats, r] = await Promise.all([
        fetchModerationOverview(),
        fetchModerationQueueStats().catch(() => ({})),
        fetchModerationConfig(),
        fetchModerationCategories(),
        fetchModerationRules(),
      ])
      setOverview(o)
      setQueueStats(q)
      setConfig(c)
      setCategories(cats)
      setRules(r)
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t('Failed to load moderation data')
      )
    }
  }

  const reloadIncidents = async (page = incidents.page) => {
    setIncidentsLoading(true)
    try {
      const res = await fetchModerationIncidents({
        page,
        pageSize: PAGE_SIZE,
      })
      setIncidents(res)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to load incidents')
      )
    } finally {
      setIncidentsLoading(false)
    }
  }

  useEffect(() => {
    reload()
    reloadIncidents(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const saveConfig = async () => {
    setSavingConfig(true)
    try {
      await updateModerationConfig(config)
      toast.success(t('Moderation config saved'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    } finally {
      setSavingConfig(false)
    }
  }

  const handleSaveRule = async () => {
    if (!editingRule.name.trim() || !editingRule.category) {
      toast.error(t('Name and category are required'))
      return
    }
    setSavingRule(true)
    try {
      await saveModerationRule(editingRule)
      toast.success(t('Rule saved'))
      setEditorOpen(false)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    } finally {
      setSavingRule(false)
    }
  }

  const handleDeleteRule = async (rule: ModerationRule) => {
    try {
      await deleteModerationRule(rule.id)
      toast.success(t('Deleted'))
      setPendingDelete(null)
      reload()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Delete failed'))
    }
  }

  const runDebug = async () => {
    if (!debugText.trim() && !debugImages.trim()) {
      toast.error(t('Provide text or image URLs to test'))
      return
    }
    setDebugRunning(true)
    try {
      const res = await debugModeration({
        text: debugText.trim() || undefined,
        images: debugImages.trim() || undefined,
        group: debugGroup.trim() || undefined,
      })
      setDebugResult(res)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Moderation debug failed'))
    } finally {
      setDebugRunning(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(incidents.total / PAGE_SIZE))

  return (
    <div className='space-y-5'>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <OverviewCard
          title={t('Rules')}
          value={overview.rule_count ?? 0}
          hint={t('{{n}} enabled', { n: overview.enabled_rule_count ?? 0 })}
        />
        <OverviewCard
          title={t('In flight')}
          value={queueStats.in_flight ?? 0}
        />
        <OverviewCard
          title={t('Queued')}
          value={queueStats.queued ?? 0}
        />
        <OverviewCard
          title={t('Blocked (24h)')}
          value={queueStats.blocked_24h ?? 0}
          hint={t('Processed: {{n}}', { n: queueStats.processed_24h ?? 0 })}
        />
      </div>

      <Tabs defaultValue='config' className='space-y-4'>
        <TabsList>
          <TabsTrigger value='config'>{t('Configuration')}</TabsTrigger>
          <TabsTrigger value='rules'>{t('Rules')}</TabsTrigger>
          <TabsTrigger value='incidents'>{t('Incidents')}</TabsTrigger>
          <TabsTrigger value='debug'>{t('Debug')}</TabsTrigger>
        </TabsList>

        {/* Config */}
        <TabsContent value='config' className='space-y-4'>
          <div className='bg-card space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-3 rounded-lg border p-3'>
              <div className='space-y-0.5'>
                <div className='text-sm font-medium'>{t('Enabled')}</div>
                <div className='text-muted-foreground text-xs'>
                  {t('Master switch for content moderation')}
                </div>
              </div>
              <Switch
                checked={Boolean(config.enabled)}
                onCheckedChange={(v) => setConfig((p) => ({ ...p, enabled: v }))}
              />
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Field label={t('Provider')}>
                <Input
                  value={String(config.provider ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({ ...p, provider: e.target.value }))
                  }
                  placeholder='openai'
                />
              </Field>
              <Field label={t('Endpoint')}>
                <Input
                  value={String(config.endpoint ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({ ...p, endpoint: e.target.value }))
                  }
                  placeholder='https://api.openai.com'
                />
              </Field>
              <Field label={t('Model')}>
                <Input
                  value={String(config.model ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({ ...p, model: e.target.value }))
                  }
                  placeholder='omni-moderation-latest'
                />
              </Field>
              <Field label={t('Default action')}>
                <Select
                  value={String(config.default_action ?? 'observe')}
                  onValueChange={(v) =>
                    setConfig((p) => ({
                      ...p,
                      default_action: v as 'observe' | 'block',
                    }))
                  }
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value='observe'>{t('Observe')}</SelectItem>
                    <SelectItem value='block'>{t('Block')}</SelectItem>
                  </SelectContent>
                </Select>
              </Field>
              <Field label={t('Request timeout (ms)')}>
                <Input
                  type='number'
                  value={String(config.request_timeout_ms ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      request_timeout_ms: Number(e.target.value) || 0,
                    }))
                  }
                />
              </Field>
              <Field label={t('Max concurrent')}>
                <Input
                  type='number'
                  value={String(config.max_concurrent ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      max_concurrent: Number(e.target.value) || 0,
                    }))
                  }
                />
              </Field>
              <Field label={t('Cache TTL (seconds)')}>
                <Input
                  type='number'
                  value={String(config.cache_ttl_seconds ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      cache_ttl_seconds: Number(e.target.value) || 0,
                    }))
                  }
                />
              </Field>
              <Field label={t('Block status code')}>
                <Input
                  type='number'
                  value={String(config.block_response_status_code ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      block_response_status_code:
                        Number(e.target.value) || 451,
                    }))
                  }
                />
              </Field>
              <Field label={t('Block message')} className='sm:col-span-2'>
                <Input
                  value={String(config.block_response_message ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      block_response_message: e.target.value,
                    }))
                  }
                />
              </Field>
              <Field label={t('Observe message')} className='sm:col-span-2'>
                <Input
                  value={String(config.observe_response_message ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      observe_response_message: e.target.value,
                    }))
                  }
                />
              </Field>
              <Field
                label={t('Provider keys (JSON, one per line)')}
                className='sm:col-span-2'
              >
                <Textarea
                  rows={4}
                  value={String(config.keys_json ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({ ...p, keys_json: e.target.value }))
                  }
                  className='font-mono text-xs'
                />
              </Field>
            </div>
            <div className='flex justify-end'>
              <Button onClick={saveConfig} disabled={savingConfig}>
                {t('Save configuration')}
              </Button>
            </div>
          </div>
        </TabsContent>

        {/* Rules */}
        <TabsContent value='rules' className='space-y-3'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='text-muted-foreground text-sm'>
              {t('Total {{n}} rules', { n: rules.length })}
            </div>
            <div className='flex gap-2'>
              <Button variant='outline' size='sm' onClick={reload}>
                <RefreshCw className='mr-1.5 h-3.5 w-3.5' />
                {t('Refresh')}
              </Button>
              <Button
                size='sm'
                onClick={() => {
                  setEditingRule({
                    ...EMPTY_RULE,
                    category: categories[0]?.name || '',
                    threshold: categories[0]?.default_threshold ?? 0.5,
                  })
                  setEditorOpen(true)
                }}
                disabled={categories.length === 0}
              >
                <Plus className='mr-1.5 h-3.5 w-3.5' />
                {t('Create rule')}
              </Button>
            </div>
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Name')}</TableHead>
                  <TableHead>{t('Category')}</TableHead>
                  <TableHead className='w-24'>{t('Threshold')}</TableHead>
                  <TableHead className='w-20'>{t('Action')}</TableHead>
                  <TableHead className='w-20'>{t('Enabled')}</TableHead>
                  <TableHead className='w-32'>{t('Operations')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={6}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('No rules')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rules.map((r) => (
                    <TableRow key={r.id}>
                      <TableCell>
                        <div className='font-medium'>{r.name}</div>
                        {r.description && (
                          <div className='text-muted-foreground line-clamp-1 text-xs'>
                            {r.description}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {r.category}
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {r.threshold}
                      </TableCell>
                      <TableCell>
                        <DecisionBadge decision={r.action} />
                      </TableCell>
                      <TableCell>
                        <Switch
                          checked={r.enabled}
                          onCheckedChange={async (v) => {
                            try {
                              await saveModerationRule({ ...r, enabled: v })
                              reload()
                            } catch (e) {
                              toast.error(
                                e instanceof Error ? e.message : t('Save failed')
                              )
                            }
                          }}
                        />
                      </TableCell>
                      <TableCell>
                        <div className='flex gap-1'>
                          <Button
                            size='sm'
                            variant='ghost'
                            onClick={() => {
                              setEditingRule(r)
                              setEditorOpen(true)
                            }}
                          >
                            {t('Edit')}
                          </Button>
                          <Button
                            size='icon'
                            variant='ghost'
                            className='text-muted-foreground hover:text-destructive h-8 w-8'
                            onClick={() => setPendingDelete(r)}
                          >
                            <Trash2 className='h-3.5 w-3.5' />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </TabsContent>

        {/* Incidents */}
        <TabsContent value='incidents' className='space-y-3'>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-40'>{t('Time')}</TableHead>
                  <TableHead>{t('Rule')}</TableHead>
                  <TableHead>{t('Category')}</TableHead>
                  <TableHead className='w-20'>{t('Score')}</TableHead>
                  <TableHead className='w-20'>{t('Decision')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Excerpt')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {incidentsLoading && incidents.items.length === 0 ? (
                  [0, 1, 2].map((i) => (
                    <TableRow key={i}>
                      {Array.from({ length: 7 }).map((_, j) => (
                        <TableCell key={j}>
                          <Skeleton className='h-4 w-full' />
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                ) : incidents.items.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={7}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('No incidents')}
                    </TableCell>
                  </TableRow>
                ) : (
                  incidents.items.map((it) => (
                    <TableRow key={it.id}>
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatRiskTime(it.created_time)}
                      </TableCell>
                      <TableCell>{it.rule_name || '—'}</TableCell>
                      <TableCell className='font-mono text-xs'>
                        {it.category || '—'}
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {it.score?.toFixed(3) ?? '—'}
                      </TableCell>
                      <TableCell>
                        <DecisionBadge decision={it.decision} />
                      </TableCell>
                      <TableCell>{it.username || '—'}</TableCell>
                      <TableCell className='text-muted-foreground max-w-[260px] truncate text-xs'>
                        {it.text_excerpt || '—'}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          <PaginationFooter
            total={incidents.total}
            page={incidents.page}
            totalPages={totalPages}
            onPage={(p) => {
              setIncidents((prev) => ({ ...prev, page: p }))
              reloadIncidents(p)
            }}
          />
        </TabsContent>

        {/* Debug */}
        <TabsContent value='debug' className='space-y-3'>
          <div className='bg-card space-y-3 rounded-xl border p-4'>
            <Field label={t('Text to test')}>
              <Textarea
                rows={4}
                value={debugText}
                onChange={(e) => setDebugText(e.target.value)}
              />
            </Field>
            <Field label={t('Image URLs (one per line)')}>
              <Textarea
                rows={3}
                value={debugImages}
                onChange={(e) => setDebugImages(e.target.value)}
                className='font-mono text-xs'
              />
            </Field>
            <Field label={t('Group')}>
              <Input
                value={debugGroup}
                onChange={(e) => setDebugGroup(e.target.value)}
                placeholder={t('Optional group name to scope rules')}
              />
            </Field>
            <div className='flex justify-end'>
              <Button onClick={runDebug} disabled={debugRunning}>
                {t('Run moderation')}
              </Button>
            </div>
            {debugResult != null && (
              <div className='bg-muted/40 rounded-md p-3'>
                <pre className='overflow-x-auto whitespace-pre-wrap text-xs'>
                  {JSON.stringify(debugResult, null, 2)}
                </pre>
              </div>
            )}
          </div>
        </TabsContent>
      </Tabs>

      {/* Rule editor */}
      <Dialog open={editorOpen} onOpenChange={setEditorOpen}>
        <DialogContent className='sm:max-w-lg'>
          <DialogHeader>
            <DialogTitle>
              {editingRule.id > 0
                ? t('Edit moderation rule')
                : t('Create moderation rule')}
            </DialogTitle>
            <DialogDescription>
              {t('Trigger when category score crosses threshold')}
            </DialogDescription>
          </DialogHeader>
          <div className='space-y-3'>
            <Field label={t('Name')}>
              <Input
                value={editingRule.name}
                onChange={(e) =>
                  setEditingRule((p) => ({ ...p, name: e.target.value }))
                }
              />
            </Field>
            <Field label={t('Category')}>
              <Select
                value={editingRule.category}
                onValueChange={(v) =>
                  setEditingRule((p) => ({ ...p, category: v }))
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {categories.map((c) => (
                    <SelectItem key={c.name} value={c.name}>
                      {c.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Field label={t('Threshold (0–1)')}>
                <Input
                  type='number'
                  step={0.01}
                  min={0}
                  max={1}
                  value={editingRule.threshold}
                  onChange={(e) =>
                    setEditingRule((p) => ({
                      ...p,
                      threshold: Number(e.target.value) || 0,
                    }))
                  }
                />
              </Field>
              <Field label={t('Priority')}>
                <Input
                  type='number'
                  value={editingRule.priority ?? 50}
                  onChange={(e) =>
                    setEditingRule((p) => ({
                      ...p,
                      priority: Number(e.target.value) || 50,
                    }))
                  }
                />
              </Field>
            </div>
            <Field label={t('Action')}>
              <Select
                value={editingRule.action}
                onValueChange={(v) =>
                  setEditingRule((p) => ({
                    ...p,
                    action: v as 'observe' | 'block',
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='observe'>{t('Observe')}</SelectItem>
                  <SelectItem value='block'>{t('Block')}</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label={t('Description')}>
              <Textarea
                rows={2}
                value={editingRule.description ?? ''}
                onChange={(e) =>
                  setEditingRule((p) => ({ ...p, description: e.target.value }))
                }
              />
            </Field>
            <div className='flex items-center justify-between rounded-lg border p-3'>
              <Label>{t('Enabled')}</Label>
              <Switch
                checked={editingRule.enabled}
                onCheckedChange={(v) =>
                  setEditingRule((p) => ({ ...p, enabled: v }))
                }
              />
            </div>
          </div>
          <DialogFooter>
            <Button
              variant='outline'
              onClick={() => setEditorOpen(false)}
              disabled={savingRule}
            >
              {t('Cancel')}
            </Button>
            <Button onClick={handleSaveRule} disabled={savingRule}>
              {t('Save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog
        open={pendingDelete !== null}
        onOpenChange={(o) => !o && setPendingDelete(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('Delete this rule?')}</AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete?.name}. {t('This action cannot be undone.')}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t('Cancel')}</AlertDialogCancel>
            <AlertDialogAction
              className='bg-destructive text-destructive-foreground hover:bg-destructive/90'
              onClick={() => pendingDelete && handleDeleteRule(pendingDelete)}
            >
              {t('Delete')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}

function Field({
  label,
  children,
  className,
}: {
  label: string
  children: React.ReactNode
  className?: string
}) {
  return (
    <div className={`space-y-1.5 ${className ?? ''}`}>
      <Label>{label}</Label>
      {children}
    </div>
  )
}
