import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Mail, RefreshCw } from 'lucide-react'
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
  RiskOverview,
} from '../types'
import { PaginationFooter } from './distribution-tab'

const PAGE_SIZE = 10

export function EnforcementTab() {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<RiskOverview>({})
  const [config, setConfig] = useState<EnforcementConfig>({})
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

  const reloadCounters = async (page = counters.page) => {
    setCountersLoading(true)
    try {
      const res = await fetchEnforcementCounters({ page, pageSize: PAGE_SIZE })
      setCounters(res)
    } catch (e) {
      toast.error(
        e instanceof Error ? e.message : t('Failed to load counters')
      )
    } finally {
      setCountersLoading(false)
    }
  }

  const reloadIncidents = async (page = incidents.page) => {
    setIncidentsLoading(true)
    try {
      const res = await fetchEnforcementIncidents({ page, pageSize: PAGE_SIZE })
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
    reloadCounters(1)
    reloadIncidents(1)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const saveConfig = async () => {
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
      toast.error(e instanceof Error ? e.message : t('Failed to send test email'))
    } finally {
      setSendingTest(false)
    }
  }

  const countersTotalPages = Math.max(
    1,
    Math.ceil(counters.total / PAGE_SIZE)
  )
  const incidentsTotalPages = Math.max(
    1,
    Math.ceil(incidents.total / PAGE_SIZE)
  )

  return (
    <div className='space-y-5'>
      <div className='grid grid-cols-2 gap-3 sm:grid-cols-4'>
        <OverviewCard
          title={t('Total subjects')}
          value={overview.total_subjects ?? 0}
        />
        <OverviewCard
          title={t('Auto-disabled')}
          value={overview.blocked_subjects ?? 0}
        />
        <OverviewCard
          title={t('Alerts (24h)')}
          value={overview.observed_incidents_24h ?? 0}
        />
        <OverviewCard
          title={t('Disables (24h)')}
          value={overview.blocked_incidents_24h ?? 0}
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
          <div className='bg-card space-y-4 rounded-xl border p-4'>
            <div className='flex items-start justify-between gap-3 rounded-lg border p-3'>
              <div className='space-y-0.5'>
                <div className='text-sm font-medium'>
                  {t('Auto-disable on threshold')}
                </div>
                <div className='text-muted-foreground text-xs'>
                  {t(
                    'Disable channel/user when the rule hit count exceeds the threshold'
                  )}
                </div>
              </div>
              <Switch
                checked={Boolean(config.auto_disable_enabled)}
                onCheckedChange={(v) =>
                  setConfig((p) => ({ ...p, auto_disable_enabled: v }))
                }
              />
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Field label={t('Auto-disable threshold')}>
                <Input
                  type='number'
                  value={String(config.auto_disable_threshold ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      auto_disable_threshold: Number(e.target.value) || 0,
                    }))
                  }
                />
              </Field>
              <Field label={t('Cooldown (seconds)')}>
                <Input
                  type='number'
                  value={String(config.cooldown_seconds ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      cooldown_seconds: Number(e.target.value) || 0,
                    }))
                  }
                />
              </Field>
            </div>

            <div className='flex items-start justify-between gap-3 rounded-lg border p-3'>
              <div className='space-y-0.5'>
                <div className='text-sm font-medium'>
                  {t('Email alerts')}
                </div>
                <div className='text-muted-foreground text-xs'>
                  {t('Send email when enforcement is triggered')}
                </div>
              </div>
              <Switch
                checked={Boolean(config.alert_email_enabled)}
                onCheckedChange={(v) =>
                  setConfig((p) => ({ ...p, alert_email_enabled: v }))
                }
              />
            </div>
            <div className='grid gap-3 sm:grid-cols-2'>
              <Field label={t('Alert email recipient')}>
                <Input
                  type='email'
                  value={String(config.alert_email_recipient ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      alert_email_recipient: e.target.value,
                    }))
                  }
                />
              </Field>
              <Field label={t('Webhook URL')}>
                <Input
                  value={String(config.alert_webhook_url ?? '')}
                  onChange={(e) =>
                    setConfig((p) => ({
                      ...p,
                      alert_webhook_url: e.target.value,
                    }))
                  }
                  placeholder='https://example.com/webhook'
                />
              </Field>
            </div>
            <div className='flex flex-wrap justify-end gap-2'>
              <Button
                type='button'
                variant='outline'
                onClick={sendTest}
                disabled={sendingTest}
              >
                <Mail className='mr-1.5 h-3.5 w-3.5' />
                {t('Send test email')}
              </Button>
              <Button onClick={saveConfig} disabled={savingConfig}>
                {t('Save configuration')}
              </Button>
            </div>
          </div>
        </TabsContent>

        {/* Counters */}
        <TabsContent value='counters' className='space-y-3'>
          <div className='flex items-center justify-end'>
            <Button
              variant='outline'
              size='sm'
              onClick={() => reloadCounters(counters.page)}
            >
              <RefreshCw className='mr-1.5 h-3.5 w-3.5' />
              {t('Refresh')}
            </Button>
          </div>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-20'>{t('Scope')}</TableHead>
                  <TableHead>{t('Identifier')}</TableHead>
                  <TableHead>{t('User')}</TableHead>
                  <TableHead className='w-20'>{t('Count')}</TableHead>
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
                  counters.items.map((c, i) => (
                    <TableRow key={`${c.scope}-${c.identifier}-${i}`}>
                      <TableCell className='font-mono text-xs uppercase'>
                        {c.scope}
                      </TableCell>
                      <TableCell className='font-mono text-xs break-all'>
                        {c.identifier}
                      </TableCell>
                      <TableCell>{c.username || '—'}</TableCell>
                      <TableCell className='font-mono text-xs'>
                        {c.count}
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {formatRiskTime(c.last_action_time)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
          <PaginationFooter
            total={counters.total}
            page={counters.page}
            totalPages={countersTotalPages}
            onPage={(p) => {
              setCounters((prev) => ({ ...prev, page: p }))
              reloadCounters(p)
            }}
          />
        </TabsContent>

        {/* Incidents */}
        <TabsContent value='incidents' className='space-y-3'>
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className='w-40'>{t('Time')}</TableHead>
                  <TableHead className='w-20'>{t('Scope')}</TableHead>
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
                        {formatRiskTime(it.created_time)}
                      </TableCell>
                      <TableCell className='font-mono text-xs uppercase'>
                        {it.scope}
                      </TableCell>
                      <TableCell className='font-mono text-xs break-all'>
                        {it.identifier}
                      </TableCell>
                      <TableCell>
                        <Badge
                          variant='outline'
                          className={
                            it.action === 'disable'
                              ? 'border-rose-500/40 bg-rose-500/10 text-rose-600 dark:text-rose-400'
                              : 'border-amber-500/40 bg-amber-500/10 text-amber-600 dark:text-amber-400'
                          }
                        >
                          {it.action}
                        </Badge>
                      </TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        {it.triggered_by || '—'}
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
            total={incidents.total}
            page={incidents.page}
            totalPages={incidentsTotalPages}
            onPage={(p) => {
              setIncidents((prev) => ({ ...prev, page: p }))
              reloadIncidents(p)
            }}
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
