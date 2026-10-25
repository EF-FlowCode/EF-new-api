import { useEffect, useState } from 'react'
import { getRouteApi } from '@tanstack/react-router'
import { Mail, RefreshCw } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import {
  ServerPagination,
  useLatestRequest,
  useServerPagination,
} from '@/components/data-table'
import {
  fetchEnforcementConfig,
  fetchEnforcementCounters,
  fetchEnforcementIncidents,
  fetchEnforcementOverview,
  sendEnforcementTestEmail,
  updateEnforcementConfig,
} from '../api'
import { OverviewCard } from '../lib/shared'
import { formatRiskTime } from '../lib/shared-utils'
import type {
  EnforcementConfig,
  EnforcementCounter,
  EnforcementIncident,
  Paginated,
  EnforcementOverview,
} from '../types'

const PAGE_SIZE = 10
const route = getRouteApi('/_authenticated/risk-center/')

export function EnforcementTab() {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<Partial<EnforcementOverview>>({})
  const [config, setConfig] = useState<EnforcementConfig | null>(null)
  const [savingConfig, setSavingConfig] = useState(false)
  const [sendingTest, setSendingTest] = useState(false)

  const [counters, setCounters] = useState<Paginated<EnforcementCounter>>({
    items: [],
    page: 1,
    page_size: PAGE_SIZE,
    total: 0,
  })
  const [countersLoading, setCountersLoading] = useState(false)

  const [incidents, setIncidents] = useState<Paginated<EnforcementIncident>>({
    items: [],
    page: 1,
    page_size: PAGE_SIZE,
    total: 0,
  })
  const [incidentsLoading, setIncidentsLoading] = useState(false)

  const search = route.useSearch()
  const navigate = route.useNavigate()
  const countersPagination = useServerPagination({
    search,
    navigate,
    total: counters.total,
    pageKey: 'enforcementCountersPage',
    pageSizeKey: 'enforcementCountersPageSize',
    defaultPageSize: PAGE_SIZE,
  })
  const incidentsPagination = useServerPagination({
    search,
    navigate,
    total: incidents.total,
    pageKey: 'enforcementIncidentsPage',
    pageSizeKey: 'enforcementIncidentsPageSize',
    defaultPageSize: PAGE_SIZE,
  })
  const runCountersRequest = useLatestRequest()
  const runIncidentsRequest = useLatestRequest()

  const reload = async () => {
    try {
      const [o, c] = await Promise.all([
        fetchEnforcementOverview(),
        fetchEnforcementConfig(),
      ])
      setOverview(o)
      setConfig(c)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to load enforcement data')
      )
    }
  }

  const reloadCounters = async () => {
    setCountersLoading(true)
    await runCountersRequest(
      (signal) =>
        fetchEnforcementCounters(
          {
            page: countersPagination.page,
            pageSize: countersPagination.pageSize,
          },
          signal
        ),
      {
        onSuccess: setCounters,
        onError: (error) =>
          toast.error(
            error instanceof Error
              ? error.message
              : t('Failed to load counters')
          ),
        onSettled: () => setCountersLoading(false),
      }
    )
  }

  const reloadIncidents = async () => {
    setIncidentsLoading(true)
    await runIncidentsRequest(
      (signal) =>
        fetchEnforcementIncidents(
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
    reloadCounters()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [countersPagination.page, countersPagination.pageSize])

  useEffect(() => {
    reloadIncidents()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [incidentsPagination.page, incidentsPagination.pageSize])

  const updateConfig = (patch: Partial<EnforcementConfig>) =>
    setConfig((previous) => (previous ? { ...previous, ...patch } : previous))

  const saveConfig = async () => {
    if (!config || savingConfig) return
    setSavingConfig(true)
    try {
      await updateEnforcementConfig(config)
      toast.success(t('Enforcement config saved'))
    } catch (e) {
      toast.error(e instanceof Error ? e.message : t('Save failed'))
    } finally {
      setSavingConfig(false)
    }
  }

  const sendTest = async () => {
    setSendingTest(true)
    try {
      await sendEnforcementTestEmail()
      toast.success(t('Test email sent'))
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to send test email')
      )
    } finally {
      setSendingTest(false)
    }
  }

  return (
    <div className='space-y-5'>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <OverviewCard
          title={t('Hits (24h)')}
          value={overview.hits_24h ?? '—'}
        />
        <OverviewCard
          title={t('Disables (24h)')}
          value={overview.auto_bans_24h ?? '—'}
        />
        <OverviewCard
          title={t('Auto-disable threshold')}
          value={overview.ban_threshold ?? '—'}
        />
        <OverviewCard
          title={t('Count window (hours)')}
          value={overview.window_hours ?? '—'}
        />
      </div>

      <Tabs defaultValue='config' className='space-y-4'>
        <TabsList>
          <TabsTrigger value='config'>{t('Configuration')}</TabsTrigger>
          <TabsTrigger value='counters'>{t('Counters')}</TabsTrigger>
          <TabsTrigger value='incidents'>{t('Incidents')}</TabsTrigger>
        </TabsList>

        {/* Config */}
        <TabsContent value='config' className='space-y-4'>
          {config ? (
            <fieldset
              disabled={savingConfig}
              className='bg-card space-y-4 rounded-xl border p-4'
            >
              <Field label={t('Enabled')}>
                <Switch
                  checked={config.enabled}
                  disabled={savingConfig}
                  onCheckedChange={(enabled) => updateConfig({ enabled })}
                />
              </Field>
              <div className='grid gap-3 sm:grid-cols-2'>
                <Field label={t('Auto-disable threshold')}>
                  <Input
                    type='number'
                    min={0}
                    value={config.ban_threshold}
                    onChange={(e) =>
                      updateConfig({ ban_threshold: Number(e.target.value) })
                    }
                  />
                </Field>
                <Field label={t('Count window (hours)')}>
                  <Input
                    type='number'
                    min={0}
                    value={config.count_window_hours}
                    onChange={(e) =>
                      updateConfig({
                        count_window_hours: Number(e.target.value),
                      })
                    }
                  />
                </Field>
                <Field label={t('Email on hit')}>
                  <Switch
                    checked={config.email_on_hit}
                    disabled={savingConfig}
                    onCheckedChange={(email_on_hit) =>
                      updateConfig({ email_on_hit })
                    }
                  />
                </Field>
                <Field label={t('Email on auto-ban')}>
                  <Switch
                    checked={config.email_on_auto_ban}
                    disabled={savingConfig}
                    onCheckedChange={(email_on_auto_ban) =>
                      updateConfig({ email_on_auto_ban })
                    }
                  />
                </Field>
              </div>
              <div className='flex flex-wrap justify-end gap-2'>
                <Button
                  type='button'
                  variant='outline'
                  onClick={sendTest}
                  disabled={sendingTest || savingConfig}
                >
                  <Mail className='mr-1.5 h-3.5 w-3.5' />
                  {t('Send test email')}
                </Button>
                <Button onClick={saveConfig} disabled={savingConfig}>
                  {t('Save configuration')}
                </Button>
              </div>
            </fieldset>
          ) : (
            <Button variant='outline' onClick={reload}>
              {t('Refresh')}
            </Button>
          )}
        </TabsContent>

        {/* Counters */}
        <TabsContent value='counters' className='space-y-3'>
          <div className='flex items-center justify-end'>
            <Button variant='outline' size='sm' onClick={reloadCounters}>
              <RefreshCw className='mr-1.5 h-3.5 w-3.5' />
              {t('Refresh')}
            </Button>
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Identifier')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead>{t('Risk hits')}</TableHead>
                  <TableHead>{t('Moderation hits')}</TableHead>
                  <TableHead className='w-40'>{t('Last action')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {countersLoading && counters.items.length === 0 ? (
                  [0, 1, 2].map((i) => (
                    <TableRow key={i}>
                      {Array.from({ length: 5 }).map((_, j) => (
                        <TableCell key={j}>
                          <Skeleton className='h-4 w-full' />
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                ) : counters.items.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={5}
                      className='text-muted-foreground py-8 text-center text-sm'
                    >
                      {t('No counters')}
                    </TableCell>
                  </TableRow>
                ) : (
                  counters.items.map((c) => (
                    <TableRow key={c.id}>
                      <TableCell>{c.id}</TableCell>
                      <TableCell>{c.username || '—'}</TableCell>
                      <TableCell>{c.enforcement_hit_count_risk}</TableCell>
                      <TableCell>
                        {c.enforcement_hit_count_moderation}
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatRiskTime(c.enforcement_last_hit_at)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          <ServerPagination
            table={countersPagination.table}
            total={counters.total}
            loading={countersLoading}
          />
        </TabsContent>

        {/* Incidents */}
        <TabsContent value='incidents' className='space-y-3'>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-40'>{t('Time')}</TableHead>
                  <TableHead className='w-20'>{t('Source')}</TableHead>
                  <TableHead>{t('Identifier')}</TableHead>
                  <TableHead className='w-24'>{t('Action')}</TableHead>
                  <TableHead>{t('Trigger')}</TableHead>
                  <TableHead>{t('Reason')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {incidentsLoading && incidents.items.length === 0 ? (
                  [0, 1, 2].map((i) => (
                    <TableRow key={i}>
                      {Array.from({ length: 6 }).map((_, j) => (
                        <TableCell key={j}>
                          <Skeleton className='h-4 w-full' />
                        </TableCell>
                      ))}
                    </TableRow>
                  ))
                ) : incidents.items.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={6}
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
                      <TableCell className='font-mono text-xs uppercase'>
                        {it.source}
                      </TableCell>
                      <TableCell className='font-mono text-xs break-all'>
                        {it.user_id}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant='outline'
                          className={
                            it.action === 'auto_ban'
                              ? 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
                              : 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
                          }
                        >
                          {it.action}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {it.rule_hint || '—'}
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
          <ServerPagination
            table={incidentsPagination.table}
            total={incidents.total}
            loading={incidentsLoading}
          />
        </TabsContent>
      </Tabs>
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
