import { useEffect, useRef, useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
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
  SelectGroup,
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
import {
  ServerPagination,
  useLatestRequest,
  useServerPagination,
} from '@/components/data-table'
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
import {
  parseModerationKeys,
  parseModerationConditions,
} from '../lib/config-contract'
import { DecisionBadge, OverviewCard } from '../lib/shared'
import { formatRiskTime } from '../lib/shared-utils'
import type {
  ModerationCategory,
  ModerationConfig,
  ModerationIncident,
  ModerationQueueStats,
  ModerationRule,
  Paginated,
  ModerationOverview,
} from '../types'

const PAGE_SIZE = 10
const route = getRouteApi('/_authenticated/risk-center/')

const EMPTY_RULE: ModerationRule = {
  id: 0,
  name: '',
  enabled: true,
  conditions: [],
  match_mode: 'all',
  score_weight: 1,
  action: 'observe',
  groups: [],
  description: '',
  priority: 50,
}

export function ModerationTab() {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<Partial<ModerationOverview>>({})
  const [queueStats, setQueueStats] = useState<ModerationQueueStats>({})
  const [config, setConfig] = useState<ModerationConfig | null>(null)
  const [keysText, setKeysText] = useState('[]')
  const [categories, setCategories] = useState<ModerationCategory[]>([])
  const [rules, setRules] = useState<ModerationRule[]>([])
  const [savingConfig, setSavingConfig] = useState(false)

  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<ModerationRule>(EMPTY_RULE)
  const [conditionsText, setConditionsText] = useState('[]')
  const [savingRule, setSavingRule] = useState(false)
  const [pendingDelete, setPendingDelete] = useState<ModerationRule | null>(
    null
  )

  const [incidents, setIncidents] = useState<Paginated<ModerationIncident>>({
    items: [],
    page: 1,
    page_size: PAGE_SIZE,
    total: 0,
  })
  const [incidentsLoading, setIncidentsLoading] = useState(false)

  const search = route.useSearch()
  const navigate = route.useNavigate()
  const incidentsPagination = useServerPagination({
    search,
    navigate,
    total: incidents.total,
    pageKey: 'moderationIncidentsPage',
    pageSizeKey: 'moderationIncidentsPageSize',
    defaultPageSize: PAGE_SIZE,
  })
  const runIncidentsRequest = useLatestRequest()

  const [debugText, setDebugText] = useState('')
  const [debugImages, setDebugImages] = useState('')
  const [debugGroup, setDebugGroup] = useState('')
  const [debugRunning, setDebugRunning] = useState(false)
  const [debugResult, setDebugResult] = useState<unknown>(null)

  const configLoading = useRef(false)
  const reload = async () => {
    if (!config && !configLoading.current) {
      configLoading.current = true
      fetchModerationConfig()
        .then((loaded) => {
          setConfig(loaded)
          setKeysText(JSON.stringify(loaded.api_keys, null, 2))
        })
        .catch((error: unknown) =>
          toast.error(
            error instanceof Error
              ? error.message
              : t('Failed to load moderation data')
          )
        )
        .finally(() => {
          configLoading.current = false
        })
    }
    try {
      const [o, q, cats, r] = await Promise.all([
        fetchModerationOverview(),
        fetchModerationQueueStats().catch(() => ({})),
        fetchModerationCategories(),
        fetchModerationRules(),
      ])
      setOverview(o)
      setQueueStats(q)
      setCategories(cats)
      setRules(r)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to load moderation data')
      )
    }
  }

  const reloadIncidents = async () => {
    setIncidentsLoading(true)
    await runIncidentsRequest(
      (signal) =>
        fetchModerationIncidents(
          {
            page: incidentsPagination.page,
            pageSize: incidentsPagination.pageSize,
          },
          signal
        ),
      {
        onSuccess: setIncidents,
        onError: (error) =>
          toast.error(
            error instanceof Error
              ? error.message
              : t('Failed to load incidents')
          ),
        onSettled: () => setIncidentsLoading(false),
      }
    )
  }

  useEffect(() => {
    reload()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    reloadIncidents()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [incidentsPagination.page, incidentsPagination.pageSize])

  const updateConfig = (patch: Partial<ModerationConfig>) => {
    setConfig((previous) => (previous ? { ...previous, ...patch } : previous))
  }

  const saveConfig = async () => {
    if (!config || savingConfig) return
    let keys: string[]
    try {
      keys = parseModerationKeys(keysText)
    } catch {
      toast.error(t('Invalid JSON'))
      return
    }
    setSavingConfig(true)
    try {
      await updateModerationConfig({ ...config, api_keys: keys })
      toast.success(t('Moderation config saved'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    } finally {
      setSavingConfig(false)
    }
  }

  const handleSaveRule = async () => {
    if (!editingRule.name.trim()) {
      toast.error(t('Please enter a rule name'))
      return
    }
    setSavingRule(true)
    try {
      await saveModerationRule({
        ...editingRule,
        conditions: parseModerationConditions(conditionsText),
      })
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

  return (
    <div className='space-y-5'>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-3'>
        <OverviewCard title={t('Rules')} value={overview.rule_count ?? '—'} />
        <OverviewCard
          title={t('Unconfigured rules')}
          value={overview.unconfigured_rule_count ?? '—'}
        />
        <OverviewCard
          title={t('Flagged (24h)')}
          value={overview.flagged_24h ?? '—'}
        />
        <OverviewCard
          title={t('In flight')}
          value={
            queueStats.worker_state?.filter(
              (worker) => worker.state === 'processing'
            ).length ?? '—'
          }
        />
        <OverviewCard
          title={t('Memory queue')}
          value={queueStats.queue_depth_memory ?? '—'}
        />
        <OverviewCard
          title={t('Redis WAL queue')}
          value={queueStats.queue_depth_redis ?? '—'}
        />
      </div>

      <Tabs defaultValue='config' className='space-y-4'>
        <TabsList>
          <TabsTrigger value='config'>{t('Configuration')}</TabsTrigger>
          <TabsTrigger value='rules'>{t('Rules')}</TabsTrigger>
          <TabsTrigger value='incidents'>{t('Incidents')}</TabsTrigger>
          <TabsTrigger value='debug'>{t('Debug')}</TabsTrigger>
        </TabsList>

        <TabsContent value='config' className='flex flex-col gap-4'>
          {config ? (
            <div className='flex flex-col gap-4 rounded-xl border p-4'>
              <Field label={t('Enabled')}>
                <Switch
                  checked={config.enabled}
                  onCheckedChange={(enabled) => updateConfig({ enabled })}
                  disabled={savingConfig}
                />
              </Field>
              <div className='grid gap-3 sm:grid-cols-2'>
                <Field label={t('Endpoint')}>
                  <Input
                    value={config.base_url}
                    onChange={(e) => updateConfig({ base_url: e.target.value })}
                    disabled={savingConfig}
                  />
                </Field>
                <Field label={t('Model')}>
                  <Input
                    value={config.model}
                    onChange={(e) => updateConfig({ model: e.target.value })}
                    disabled={savingConfig}
                  />
                </Field>
                <Field label={t('Mode')}>
                  <Select
                    value={config.mode}
                    onValueChange={(mode) =>
                      updateConfig({ mode: mode as ModerationConfig['mode'] })
                    }
                    disabled={savingConfig}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        <SelectItem value='off'>{t('Disabled')}</SelectItem>
                        <SelectItem value='observe_only'>
                          {t('Observe')}
                        </SelectItem>
                        <SelectItem value='enforce'>
                          {t('Enforce moderation')}
                        </SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
                <Field label={t('Request timeout (ms)')}>
                  <Input
                    type='number'
                    value={config.http_timeout_ms}
                    onChange={(e) =>
                      updateConfig({ http_timeout_ms: Number(e.target.value) })
                    }
                    disabled={savingConfig}
                  />
                </Field>
                <Field label={t('Max concurrent')}>
                  <Input
                    type='number'
                    value={config.worker_count}
                    onChange={(e) =>
                      updateConfig({ worker_count: Number(e.target.value) })
                    }
                    disabled={savingConfig}
                  />
                </Field>
                <Field
                  label={t('Moderation API keys (JSON array)')}
                  className='sm:col-span-2'
                >
                  <Textarea
                    rows={4}
                    value={keysText}
                    onChange={(e) => setKeysText(e.target.value)}
                    className='font-mono text-xs'
                    disabled={savingConfig}
                  />
                </Field>
              </div>
              <div className='flex justify-end'>
                <Button onClick={saveConfig} disabled={savingConfig}>
                  {t('Save configuration')}
                </Button>
              </div>
            </div>
          ) : (
            <Skeleton className='h-48 w-full' />
          )}
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
                  const rule = {
                    ...EMPTY_RULE,
                    conditions: [
                      {
                        category: categories[0]?.name || '',
                        op: '>=',
                        value: 0.5,
                      },
                    ],
                  }
                  setEditingRule(rule)
                  setConditionsText(JSON.stringify(rule.conditions, null, 2))
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
                  <TableHead className='w-24'>{t('Match mode')}</TableHead>
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
                        {r.conditions
                          .map(
                            (condition) =>
                              `${condition.category} ${condition.op} ${condition.value}`
                          )
                          .join('; ')}
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {r.match_mode}
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
                                e instanceof Error
                                  ? e.message
                                  : t('Save failed')
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
                              setConditionsText(
                                JSON.stringify(r.conditions, null, 2)
                              )
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
                        {formatRiskTime(it.created_at)}
                      </TableCell>
                      <TableCell>{it.primary_rule || '—'}</TableCell>
                      <TableCell className='font-mono text-xs'>
                        {it.max_category || '—'}
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {it.max_score?.toFixed(3) ?? '—'}
                      </TableCell>
                      <TableCell>
                        <DecisionBadge decision={it.decision} />
                      </TableCell>
                      <TableCell>{it.username || '—'}</TableCell>
                      <TableCell className='text-muted-foreground max-w-[260px] truncate text-xs'>
                        {it.input_summary || '—'}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          <ServerPagination
            table={incidentsPagination.table}
            total={incidents.total}
            loading={incidentsLoading}
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
                <pre className='overflow-x-auto text-xs whitespace-pre-wrap'>
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
            <Field label={t('Moderation conditions (JSON array)')}>
              <Textarea
                rows={8}
                value={conditionsText}
                onChange={(e) => setConditionsText(e.target.value)}
                className='font-mono text-xs'
              />
            </Field>
            <Field label={t('Groups')}>
              <Input
                value={editingRule.groups.join(',')}
                onChange={(e) =>
                  setEditingRule((previous) => ({
                    ...previous,
                    groups: e.target.value.split(','),
                  }))
                }
              />
            </Field>
            <Field label={t('Match mode')}>
              <Select
                value={editingRule.match_mode}
                onValueChange={(mode) =>
                  setEditingRule((previous) => ({
                    ...previous,
                    match_mode: mode as 'all' | 'any',
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value='all'>{t('All')}</SelectItem>
                    <SelectItem value='any'>{t('Any')}</SelectItem>
                  </SelectGroup>
                </SelectContent>
              </Select>
            </Field>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Field label={t('Score weight')}>
                <Input
                  type='number'
                  value={editingRule.score_weight}
                  onChange={(e) =>
                    setEditingRule((previous) => ({
                      ...previous,
                      score_weight: Number(e.target.value),
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
                    action: v as ModerationRule['action'],
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='observe'>{t('Observe')}</SelectItem>
                  <SelectItem value='flag'>{t('Flag')}</SelectItem>
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
