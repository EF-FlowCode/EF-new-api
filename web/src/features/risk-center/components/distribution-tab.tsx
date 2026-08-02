import { useEffect, useMemo, useState } from 'react'
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
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
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
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import {
  deleteRule,
  detectIp,
  fetchIncidents,
  fetchOverview,
  fetchRiskGroups,
  fetchRules,
  fetchSubjects,
  updateRule,
} from '../api'
import { DecisionBadge, OverviewCard, StatusBadge } from '../lib/shared'
import { formatRiskTime, metricLabel } from '../lib/shared-utils'
import type {
  Paginated,
  RiskGroups,
  RiskIncident,
  RiskOverview,
  RiskRule,
  RiskSubject,
} from '../types'
import { RiskRuleDialog } from './risk-rule-dialog'

const PAGE_SIZE = 10

export function DistributionTab() {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<RiskOverview>({})
  const [groups, setGroups] = useState<RiskGroups>({ items: [] })
  const [rules, setRules] = useState<RiskRule[]>([])
  const [loading, setLoading] = useState(true)

  const [subjectsPage, setSubjectsPage] = useState<Paginated<RiskSubject>>({
    items: [],
    page: 1,
    page_size: PAGE_SIZE,
    total: 0,
  })
  const [subjectFilters, setSubjectFilters] = useState({
    scope: 'all',
    status: 'all',
    keyword: '',
  })
  const [subjectsLoading, setSubjectsLoading] = useState(false)

  const [incidentsPage, setIncidentsPage] = useState<Paginated<RiskIncident>>({
    items: [],
    page: 1,
    page_size: PAGE_SIZE,
    total: 0,
  })
  const [incidentFilters, setIncidentFilters] = useState({
    scope: 'all',
    action: 'all',
    keyword: '',
  })
  const [incidentsLoading, setIncidentsLoading] = useState(false)

  const [editorOpen, setEditorOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<RiskRule | null>(null)
  const [pendingDelete, setPendingDelete] = useState<RiskRule | null>(null)

  const [ipInput, setIpInput] = useState('')
  const [detecting, setDetecting] = useState(false)
  const [detectResult, setDetectResult] = useState<unknown>(null)

  const groupOptions = useMemo(
    () => groups.items.map((g) => g.name),
    [groups.items]
  )

  const reloadOverview = async () => {
    try {
      const [o, g, r] = await Promise.all([
        fetchOverview(),
        fetchRiskGroups(),
        fetchRules(),
      ])
      setOverview(o)
      setGroups(g)
      setRules(r)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to load overview'))
    }
  }

  const reloadSubjects = async (
    page = subjectsPage.page,
    filters = subjectFilters
  ) => {
    setSubjectsLoading(true)
    try {
      const res = await fetchSubjects({
        page,
        pageSize: PAGE_SIZE,
        scope: filters.scope === 'all' ? '' : filters.scope,
        status: filters.status === 'all' ? '' : filters.status,
        keyword: filters.keyword,
      })
      setSubjectsPage(res)
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Failed to load subjects'))
    } finally {
      setSubjectsLoading(false)
    }
  }

  const reloadIncidents = async (
    page = incidentsPage.page,
    filters = incidentFilters
  ) => {
    setIncidentsLoading(true)
    try {
      const res = await fetchIncidents({
        page,
        pageSize: PAGE_SIZE,
        scope: filters.scope === 'all' ? '' : filters.scope,
        action: filters.action === 'all' ? '' : filters.action,
        keyword: filters.keyword,
      })
      setIncidentsPage(res)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to load incidents')
      )
    } finally {
      setIncidentsLoading(false)
    }
  }

  useEffect(() => {
    let active = true
    setLoading(true)
    Promise.all([reloadOverview(), reloadSubjects(1), reloadIncidents(1)])
      .finally(() => {
        if (active) setLoading(false)
      })
    return () => {
      active = false
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const handleToggleRule = async (rule: RiskRule) => {
    try {
      // Backend expects conditions/groups as JSON strings; rule.conditions
      // and rule.groups are already-parsed arrays in memory.
      const payload = {
        ...rule,
        enabled: !rule.enabled,
        conditions: JSON.stringify(rule.conditions),
        groups: JSON.stringify(rule.groups),
      } as unknown as Partial<RiskRule>
      await updateRule(rule.id, payload)
      toast.success(t('Rule updated'))
      reloadOverview()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    }
  }

  const handleDeleteRule = async (rule: RiskRule) => {
    try {
      await deleteRule(rule.id)
      toast.success(t('Deleted'))
      setPendingDelete(null)
      reloadOverview()
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Delete failed'))
    }
  }

  const handleDetectIP = async () => {
    const ip = ipInput.trim()
    if (!ip) return
    setDetecting(true)
    try {
      const r = await detectIp(ip)
      setDetectResult(r)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to detect IP risk')
      )
    } finally {
      setDetecting(false)
    }
  }

  const subjectsTotalPages = Math.max(
    1,
    Math.ceil(subjectsPage.total / PAGE_SIZE)
  )
  const incidentsTotalPages = Math.max(
    1,
    Math.ceil(incidentsPage.total / PAGE_SIZE)
  )

  return (
    <div className='space-y-5'>
      {/* Overview cards */}
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <OverviewCard
          title={t('Total subjects')}
          value={loading ? <Skeleton className='h-8 w-16' /> : overview.total_subjects ?? 0}
        />
        <OverviewCard
          title={t('Blocked')}
          value={loading ? <Skeleton className='h-8 w-16' /> : overview.blocked_subjects ?? 0}
        />
        <OverviewCard
          title={t('Observing')}
          value={loading ? <Skeleton className='h-8 w-16' /> : overview.observed_subjects ?? 0}
        />
        <OverviewCard
          title={t('Incidents (24h)')}
          value={loading ? <Skeleton className='h-8 w-16' /> : overview.total_incidents_24h ?? 0}
          hint={t('Block: {{a}} · Observe: {{b}}', {
            a: overview.blocked_incidents_24h ?? 0,
            b: overview.observed_incidents_24h ?? 0,
          })}
        />
      </div>

      <Tabs defaultValue='rules' className='space-y-4'>
        <TabsList>
          <TabsTrigger value='rules'>{t('Rules')}</TabsTrigger>
          <TabsTrigger value='subjects'>{t('Subjects')}</TabsTrigger>
          <TabsTrigger value='incidents'>{t('Incidents')}</TabsTrigger>
          <TabsTrigger value='detect'>{t('IP detection')}</TabsTrigger>
        </TabsList>

        {/* Rules */}
        <TabsContent value='rules' className='space-y-3'>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <div className='text-muted-foreground text-sm'>
              {t('Total {{n}} rules ({{enabled}} enabled)', {
                n: rules.length,
                enabled: rules.filter((r) => r.enabled).length,
              })}
            </div>
            <div className='flex gap-2'>
              <Button variant='outline' size='sm' onClick={reloadOverview}>
                <RefreshCw className='mr-1.5 h-3.5 w-3.5' />
                {t('Refresh')}
              </Button>
              <Button
                size='sm'
                onClick={() => {
                  setEditingRule(null)
                  setEditorOpen(true)
                }}
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
                  <TableHead className='w-16'>{t('Scope')}</TableHead>
                  <TableHead className='w-20'>{t('Action')}</TableHead>
                  <TableHead className='w-20'>{t('Priority')}</TableHead>
                  <TableHead>{t('Conditions')}</TableHead>
                  <TableHead className='w-20'>{t('Enabled')}</TableHead>
                  <TableHead className='w-32'>{t('Operations')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={7}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('No rules')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rules.map((rule) => (
                    <TableRow key={rule.id}>
                      <TableCell>
                        <div className='font-medium'>{rule.name}</div>
                        {rule.description && (
                          <div className='text-muted-foreground line-clamp-1 text-xs'>
                            {rule.description}
                          </div>
                        )}
                      </TableCell>
                      <TableCell className='font-mono text-xs uppercase'>
                        {rule.scope}
                      </TableCell>
                      <TableCell>
                        <DecisionBadge decision={rule.action} />
                      </TableCell>
                      <TableCell className='font-mono text-xs'>
                        {rule.priority}
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {rule.conditions
                          .map(
                            (c) => `${metricLabel(c.metric)} ${c.op} ${c.value}`
                          )
                          .join(' / ')}
                      </TableCell>
                      <TableCell>
                        <Switch
                          checked={rule.enabled}
                          onCheckedChange={() => handleToggleRule(rule)}
                        />
                      </TableCell>
                      <TableCell>
                        <div className='flex gap-1'>
                          <Button
                            size='sm'
                            variant='ghost'
                            onClick={() => {
                              setEditingRule(rule)
                              setEditorOpen(true)
                            }}
                          >
                            {t('Edit')}
                          </Button>
                          <Button
                            size='icon'
                            variant='ghost'
                            className='text-muted-foreground hover:text-destructive h-8 w-8'
                            onClick={() => setPendingDelete(rule)}
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

        {/* Subjects */}
        <TabsContent value='subjects' className='space-y-3'>
          <div className='flex flex-wrap items-end gap-2'>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Scope')}
              </Label>
              <Select
                value={subjectFilters.scope}
                onValueChange={(v) =>
                  setSubjectFilters((p) => ({ ...p, scope: v }))
                }
              >
                <SelectTrigger className='w-32'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All scopes')}</SelectItem>
                  <SelectItem value='token'>{t('Token')}</SelectItem>
                  <SelectItem value='user'>{t('User')}</SelectItem>
                  <SelectItem value='ip'>{t('IP')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Status')}
              </Label>
              <Select
                value={subjectFilters.status}
                onValueChange={(v) =>
                  setSubjectFilters((p) => ({ ...p, status: v }))
                }
              >
                <SelectTrigger className='w-36'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All statuses')}</SelectItem>
                  <SelectItem value='normal'>{t('Normal')}</SelectItem>
                  <SelectItem value='observe'>{t('Observing')}</SelectItem>
                  <SelectItem value='blocked'>{t('Blocked')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Input
              placeholder={t('Keyword')}
              value={subjectFilters.keyword}
              onChange={(e) =>
                setSubjectFilters((p) => ({ ...p, keyword: e.target.value }))
              }
              className='max-w-[200px]'
            />
            <Button
              size='sm'
              onClick={() => {
                setSubjectsPage((p) => ({ ...p, page: 1 }))
                reloadSubjects(1)
              }}
            >
              {t('Search')}
            </Button>
          </div>
          <SubjectsTable
            data={subjectsPage}
            loading={subjectsLoading}
            totalPages={subjectsTotalPages}
            onPage={(p) => {
              setSubjectsPage((prev) => ({ ...prev, page: p }))
              reloadSubjects(p)
            }}
          />
        </TabsContent>

        {/* Incidents */}
        <TabsContent value='incidents' className='space-y-3'>
          <div className='flex flex-wrap items-end gap-2'>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Scope')}
              </Label>
              <Select
                value={incidentFilters.scope}
                onValueChange={(v) =>
                  setIncidentFilters((p) => ({ ...p, scope: v }))
                }
              >
                <SelectTrigger className='w-32'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All scopes')}</SelectItem>
                  <SelectItem value='token'>{t('Token')}</SelectItem>
                  <SelectItem value='user'>{t('User')}</SelectItem>
                  <SelectItem value='ip'>{t('IP')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className='space-y-1'>
              <Label className='text-muted-foreground text-xs'>
                {t('Decision')}
              </Label>
              <Select
                value={incidentFilters.action}
                onValueChange={(v) =>
                  setIncidentFilters((p) => ({ ...p, action: v }))
                }
              >
                <SelectTrigger className='w-36'>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value='all'>{t('All decisions')}</SelectItem>
                  <SelectItem value='allow'>{t('Allow')}</SelectItem>
                  <SelectItem value='observe'>{t('Observe')}</SelectItem>
                  <SelectItem value='block'>{t('Block')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Input
              placeholder={t('Keyword')}
              value={incidentFilters.keyword}
              onChange={(e) =>
                setIncidentFilters((p) => ({ ...p, keyword: e.target.value }))
              }
              className='max-w-[200px]'
            />
            <Button
              size='sm'
              onClick={() => {
                setIncidentsPage((p) => ({ ...p, page: 1 }))
                reloadIncidents(1)
              }}
            >
              {t('Search')}
            </Button>
          </div>
          <IncidentsTable
            data={incidentsPage}
            loading={incidentsLoading}
            totalPages={incidentsTotalPages}
            onPage={(p) => {
              setIncidentsPage((prev) => ({ ...prev, page: p }))
              reloadIncidents(p)
            }}
          />
        </TabsContent>

        {/* IP detection */}
        <TabsContent value='detect' className='space-y-3'>
          <div className='bg-card rounded-xl border p-4'>
            <div className='space-y-2'>
              <Label>{t('IP address')}</Label>
              <div className='flex gap-2'>
                <Input
                  value={ipInput}
                  onChange={(e) => setIpInput(e.target.value)}
                  placeholder='1.2.3.4'
                />
                <Button onClick={handleDetectIP} disabled={detecting || !ipInput.trim()}>
                  {t('Detect')}
                </Button>
              </div>
              <p className='text-muted-foreground text-xs'>
                {t('Probe the configured upstream risk service for the given IP.')}
              </p>
            </div>
            {detectResult != null && (
              <div className='bg-muted/40 mt-3 rounded-md p-3'>
                <pre className='overflow-x-auto whitespace-pre-wrap text-xs'>
                  {JSON.stringify(detectResult, null, 2)}
                </pre>
              </div>
            )}
          </div>
        </TabsContent>
      </Tabs>

      <RiskRuleDialog
        open={editorOpen}
        onOpenChange={setEditorOpen}
        rule={editingRule}
        groupOptions={groupOptions}
        onSaved={reloadOverview}
      />

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

// ----------------------------------------------------------------------------
// Sub-tables
// ----------------------------------------------------------------------------

function SubjectsTable({
  data,
  loading,
  totalPages,
  onPage,
}: {
  data: Paginated<RiskSubject>
  loading: boolean
  totalPages: number
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <>
      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='w-20'>{t('Scope')}</TableHead>
              <TableHead>{t('Identifier')}</TableHead>
              <TableHead>{t('User')}</TableHead>
              <TableHead className='w-24'>{t('Status')}</TableHead>
              <TableHead className='w-20'>{t('Score')}</TableHead>
              <TableHead className='w-20'>{t('Hits 24h')}</TableHead>
              <TableHead className='w-40'>{t('Last hit')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && data.items.length === 0 ? (
              [0, 1, 2].map((i) => (
                <TableRow key={i}>
                  {Array.from({ length: 7 }).map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton className='h-4 w-full' />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : data.items.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={7}
                  className='text-muted-foreground py-8 text-center text-sm'
                >
                  {t('No subjects')}
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((s) => (
                <TableRow key={s.id}>
                  <TableCell className='font-mono text-xs uppercase'>
                    {s.scope}
                  </TableCell>
                  <TableCell className='font-mono text-xs break-all'>
                    {s.identifier}
                  </TableCell>
                  <TableCell>{s.username || '—'}</TableCell>
                  <TableCell>
                    <StatusBadge status={s.status} />
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {s.risk_score}
                  </TableCell>
                  <TableCell className='font-mono text-xs'>
                    {s.hit_count_24h}
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {formatRiskTime(s.last_hit_time)}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <PaginationFooter
        total={data.total}
        page={data.page}
        totalPages={totalPages}
        onPage={onPage}
      />
    </>
  )
}

function IncidentsTable({
  data,
  loading,
  totalPages,
  onPage,
}: {
  data: Paginated<RiskIncident>
  loading: boolean
  totalPages: number
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <>
      <div className='overflow-x-auto rounded-md border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className='w-40'>{t('Time')}</TableHead>
              <TableHead className='w-20'>{t('Scope')}</TableHead>
              <TableHead>{t('Identifier')}</TableHead>
              <TableHead>{t('Rule')}</TableHead>
              <TableHead className='w-20'>{t('Decision')}</TableHead>
              <TableHead>{t('Reason')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {loading && data.items.length === 0 ? (
              [0, 1, 2].map((i) => (
                <TableRow key={i}>
                  {Array.from({ length: 6 }).map((_, j) => (
                    <TableCell key={j}>
                      <Skeleton className='h-4 w-full' />
                    </TableCell>
                  ))}
                </TableRow>
              ))
            ) : data.items.length === 0 ? (
              <TableRow>
                <TableCell
                  colSpan={6}
                  className='text-muted-foreground py-8 text-center text-sm'
                >
                  {t('No incidents')}
                </TableCell>
              </TableRow>
            ) : (
              data.items.map((it) => (
                <TableRow key={it.id}>
                  <TableCell className='text-muted-foreground text-xs'>
                    {formatRiskTime(it.created_time)}
                  </TableCell>
                  <TableCell className='font-mono text-xs uppercase'>
                    {it.scope}
                  </TableCell>
                  <TableCell className='font-mono text-xs break-all'>
                    {it.identifier}
                  </TableCell>
                  <TableCell>
                    {it.rule_name ? (
                      <span className='text-sm'>{it.rule_name}</span>
                    ) : (
                      <Badge variant='outline' className='font-normal'>
                        {t('Manual')}
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    <DecisionBadge decision={it.decision} />
                  </TableCell>
                  <TableCell className='text-muted-foreground text-xs'>
                    {it.reason || '—'}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <PaginationFooter
        total={data.total}
        page={data.page}
        totalPages={totalPages}
        onPage={onPage}
      />
    </>
  )
}

export function PaginationFooter({
  total,
  page,
  totalPages,
  onPage,
}: {
  total: number
  page: number
  totalPages: number
  onPage: (page: number) => void
}) {
  const { t } = useTranslation()
  return (
    <div className='flex flex-wrap items-center justify-between gap-3'>
      <div className='text-muted-foreground text-xs'>
        {t('Total {{n}} records', { n: total })}
      </div>
      <div className='flex items-center gap-2'>
        <Button
          size='sm'
          variant='outline'
          disabled={page <= 1}
          onClick={() => onPage(Math.max(1, page - 1))}
        >
          {t('Previous')}
        </Button>
        <span className='text-muted-foreground text-xs'>
          {t('Page {{p}} / {{total}}', { p: page, total: totalPages })}
        </span>
        <Button
          size='sm'
          variant='outline'
          disabled={page >= totalPages}
          onClick={() => onPage(Math.min(totalPages, page + 1))}
        >
          {t('Next')}
        </Button>
      </div>
    </div>
  )
}
