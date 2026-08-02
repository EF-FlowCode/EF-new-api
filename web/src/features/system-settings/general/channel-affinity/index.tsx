import { useCallback, useEffect, useMemo, useState } from 'react'
import { Edit, FileText, Plus, RefreshCw, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { ConfirmDialog } from '@/components/confirm-dialog'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { StatusBadge } from '@/components/status-badge'
import { SettingsSection } from '../../components/settings-section'
import { useUpdateOption } from '../../hooks/use-update-option'
import {
  getCacheStats,
  clearAllCache,
  clearRuleCache,
  clearGroupCache,
} from './api'
import { RULE_TEMPLATES, cloneTemplate, makeUniqueName } from './constants'
import { RuleEditorDialog } from './rule-editor-dialog'
import type { AffinityRule, CacheStats, ChannelAffinitySettings } from './types'

function parseRules(jsonStr: string): AffinityRule[] {
  try {
    const arr = JSON.parse(jsonStr || '[]')
    if (!Array.isArray(arr)) return []
    return arr.map(
      (r: Record<string, unknown>, i: number) =>
        ({ id: i, ...r }) as AffinityRule
    )
  } catch {
    return []
  }
}

function serializeRules(rules: AffinityRule[]): string {
  return JSON.stringify(rules.map(({ id: _, ...rest }) => rest))
}

interface Props {
  defaultValues: ChannelAffinitySettings
}

export function ChannelAffinitySection(props: Props) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const [enabled, setEnabled] = useState(
    props.defaultValues['channel_affinity_setting.enabled']
  )
  const [switchOnSuccess, setSwitchOnSuccess] = useState(
    props.defaultValues['channel_affinity_setting.switch_on_success']
  )
  const [failoverOnChannelDown, setFailoverOnChannelDown] = useState(
    props.defaultValues['channel_affinity_setting.failover_on_channel_down']
  )
  const [maxEntries, setMaxEntries] = useState(
    props.defaultValues['channel_affinity_setting.max_entries']
  )
  const [defaultTtl, setDefaultTtl] = useState(
    props.defaultValues['channel_affinity_setting.default_ttl_seconds']
  )
  const [rules, setRules] = useState<AffinityRule[]>(() =>
    parseRules(props.defaultValues['channel_affinity_setting.rules'])
  )

  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [jsonText, setJsonText] = useState(() =>
    JSON.stringify(
      parseRules(props.defaultValues['channel_affinity_setting.rules']).map(
        ({ id: _, ...r }) => r
      ),
      null,
      2
    )
  )
  const [cacheStats, setCacheStats] = useState<CacheStats | null>(null)
  const [cacheLoading, setCacheLoading] = useState(false)
  const [saving, setSaving] = useState(false)

  const [ruleEditorOpen, setRuleEditorOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<AffinityRule | null>(null)
  const [ruleTemplateKey, setRuleTemplateKey] = useState<string | null>(null)
  const [clearAllDialogOpen, setClearAllDialogOpen] = useState(false)
  const [clearRuleName, setClearRuleName] = useState<string | null>(null)
  const [groupToClear, setGroupToClear] = useState('')
  const [clearGroupDialogOpen, setClearGroupDialogOpen] = useState(false)
  const [fillTemplateDialogOpen, setFillTemplateDialogOpen] = useState(false)

  useEffect(() => {
    setEnabled(props.defaultValues['channel_affinity_setting.enabled'])
    setSwitchOnSuccess(
      props.defaultValues['channel_affinity_setting.switch_on_success']
    )
    setFailoverOnChannelDown(
      props.defaultValues['channel_affinity_setting.failover_on_channel_down']
    )
    setMaxEntries(props.defaultValues['channel_affinity_setting.max_entries'])
    setDefaultTtl(
      props.defaultValues['channel_affinity_setting.default_ttl_seconds']
    )
    const parsed = parseRules(
      props.defaultValues['channel_affinity_setting.rules']
    )
    setRules(parsed)
    setJsonText(
      JSON.stringify(
        parsed.map(({ id: _, ...r }) => r),
        null,
        2
      )
    )
  }, [props.defaultValues])

  const refreshCache = useCallback(async () => {
    setCacheLoading(true)
    try {
      const res = await getCacheStats()
      if (res.success) setCacheStats(res.data || null)
    } catch {
      toast.error(t('Failed to refresh cache stats'))
    } finally {
      setCacheLoading(false)
    }
  }, [t])

  useEffect(() => {
    refreshCache()
  }, [refreshCache])

  const appendCliTemplates = () => {
    const existingNames = new Set(
      rules.map((r) => (r.name || '').trim()).filter((x) => x.length > 0)
    )

    const templates = Object.values(RULE_TEMPLATES).map((tpl) => {
      const base = cloneTemplate(tpl)
      const name = makeUniqueName(existingNames, tpl.name)
      existingNames.add(name)
      return { ...base, name }
    })

    setRules((prev) =>
      [...prev, ...templates].map((r, idx) => ({ ...r, id: idx }))
    )
    toast.success(t('Templates appended'))
    setFillTemplateDialogOpen(false)
  }

  const handleFillTemplates = () => {
    if (rules.length === 0) {
      appendCliTemplates()
    } else {
      setFillTemplateDialogOpen(true)
    }
  }

  const handleSave = async () => {
    let rulesJson: string
    if (editMode === 'json') {
      try {
        const parsed = JSON.parse(jsonText)
        if (!Array.isArray(parsed)) {
          toast.error(t('Rules JSON must be an array'))
          return
        }
        rulesJson = JSON.stringify(parsed)
      } catch {
        toast.error(t('Invalid rules JSON format'))
        return
      }
    } else {
      rulesJson = serializeRules(rules)
    }

    setSaving(true)
    try {
      const updates: { key: string; value: string }[] = []

      if (enabled !== props.defaultValues['channel_affinity_setting.enabled'])
        updates.push({
          key: 'channel_affinity_setting.enabled',
          value: String(enabled),
        })
      if (
        switchOnSuccess !==
        props.defaultValues['channel_affinity_setting.switch_on_success']
      )
        updates.push({
          key: 'channel_affinity_setting.switch_on_success',
          value: String(switchOnSuccess),
        })
      if (
        failoverOnChannelDown !==
        props.defaultValues['channel_affinity_setting.failover_on_channel_down']
      )
        updates.push({
          key: 'channel_affinity_setting.failover_on_channel_down',
          value: String(failoverOnChannelDown),
        })
      if (
        maxEntries !==
        props.defaultValues['channel_affinity_setting.max_entries']
      )
        updates.push({
          key: 'channel_affinity_setting.max_entries',
          value: String(maxEntries),
        })
      if (
        defaultTtl !==
        props.defaultValues['channel_affinity_setting.default_ttl_seconds']
      )
        updates.push({
          key: 'channel_affinity_setting.default_ttl_seconds',
          value: String(defaultTtl),
        })

      const origRules = props.defaultValues['channel_affinity_setting.rules']
      const origSerialized = (() => {
        try {
          return JSON.stringify(JSON.parse(origRules || '[]'))
        } catch {
          return '[]'
        }
      })()
      if (rulesJson !== origSerialized) {
        updates.push({
          key: 'channel_affinity_setting.rules',
          value: rulesJson,
        })
      }

      if (updates.length === 0) {
        toast.info(t('No changes'))
        return
      }

      for (const u of updates) {
        await updateOption.mutateAsync(u)
      }
      toast.success(t('Saved successfully'))
    } catch {
      toast.error(t('Failed to save'))
    } finally {
      setSaving(false)
    }
  }

  const handleRuleSave = (rule: AffinityRule) => {
    setRules((prev) => {
      const existIdx = prev.findIndex(
        (r) => r.id === rule.id || (rule.name && r.name === editingRule?.name)
      )
      if (existIdx >= 0) {
        const next = [...prev]
        next[existIdx] = { ...rule, id: existIdx }
        return next
      }
      return [...prev, { ...rule, id: prev.length }]
    })
    setEditingRule(null)
  }

  const handleDeleteRule = (idx: number) => {
    setRules((prev) =>
      prev.filter((_, i) => i !== idx).map((r, i) => ({ ...r, id: i }))
    )
    toast.success(t('Deleted successfully'))
  }

  const handleClearAll = async () => {
    const res = await clearAllCache()
    if (res.success) {
      toast.success(t('Cleared'))
      refreshCache()
    }
    setClearAllDialogOpen(false)
  }

  const handleClearRule = async () => {
    if (!clearRuleName) return
    const res = await clearRuleCache(clearRuleName)
    if (res.success) {
      toast.success(t('Cleared'))
      refreshCache()
    }
    setClearRuleName(null)
  }

  const groupOptions = useMemo<ComboboxOption[]>(() => {
    const byGroup = cacheStats?.by_using_group ?? {}
    return Object.entries(byGroup)
      .sort((a, b) => b[1] - a[1])
      .map(([group, count]) => ({
        value: group,
        label: `${group} (${count})`,
      }))
  }, [cacheStats])

  const handleClearGroup = async () => {
    const group = groupToClear.trim()
    if (!group) return
    const res = await clearGroupCache(group)
    if (res.success) {
      toast.success(`${t('Cleared')}: ${res.data?.deleted ?? 0}`)
      setGroupToClear('')
      refreshCache()
    } else {
      toast.error(res.message || t('Failed to save'))
    }
    setClearGroupDialogOpen(false)
  }

  const switchToJsonMode = () => {
    setJsonText(
      JSON.stringify(
        rules.map(({ id: _, ...r }) => r),
        null,
        2
      )
    )
    setEditMode('json')
  }

  const switchToVisualMode = () => {
    try {
      const parsed = JSON.parse(jsonText)
      if (!Array.isArray(parsed)) {
        toast.error(t('Rules JSON must be an array'))
        return
      }
      setRules(
        parsed.map(
          (r: Record<string, unknown>, i: number) =>
            ({ id: i, ...r }) as AffinityRule
        )
      )
      setEditMode('visual')
    } catch {
      toast.error(t('Invalid rules JSON format'))
    }
  }

  return (
    <>
      <SettingsSection
        title={t('Channel Affinity')}
        description={t(
          'Prioritize reusing the last successful channel based on keys extracted from request context (sticky routing)'
        )}
      >
        <Alert>
          <AlertDescription className='text-xs'>
            {t(
              'Channel affinity reuses the last successful channel based on keys extracted from the request context or JSON body.'
            )}
          </AlertDescription>
        </Alert>

        {/* Basic Settings */}
        <div className='grid grid-cols-1 gap-4 md:grid-cols-3'>
          <div className='flex items-center gap-2'>
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            <Label>{t('Enable')}</Label>
          </div>
          <div className='grid gap-1.5'>
            <Label>{t('Max Entries')}</Label>
            <Input
              type='number'
              min={0}
              value={maxEntries}
              onChange={(e) => setMaxEntries(Number(e.target.value))}
            />
          </div>
          <div className='grid gap-1.5'>
            <Label>{t('Default TTL (seconds)')}</Label>
            <Input
              type='number'
              min={0}
              value={defaultTtl}
              onChange={(e) => setDefaultTtl(Number(e.target.value))}
            />
          </div>
        </div>

        <div className='flex items-center gap-2'>
          <Switch
            checked={switchOnSuccess}
            onCheckedChange={setSwitchOnSuccess}
          />
          <Label>{t('Switch affinity on success')}</Label>
          <span className='text-muted-foreground text-xs'>
            {t(
              'If the affinity channel fails and retry succeeds on another channel, update affinity to the successful channel.'
            )}
          </span>
        </div>

        <div className='flex items-center gap-2'>
          <Switch
            checked={failoverOnChannelDown}
            onCheckedChange={setFailoverOnChannelDown}
          />
          <Label>{t('Failover when affinity channel is down')}</Label>
          <span className='text-muted-foreground text-xs'>
            {t(
              'When the pinned channel returns a channel-down error, fail over to another channel even under hard affinity, and drop the stale pin so later requests recover automatically.'
            )}
          </span>
        </div>

        <Separator />

        {/* Toolbar */}
        <div className='flex flex-wrap items-center gap-2'>
          <Button
            variant={editMode === 'visual' ? 'default' : 'outline'}
            size='sm'
            onClick={editMode === 'json' ? switchToVisualMode : undefined}
          >
            {t('Visual')}
          </Button>
          <Button
            variant={editMode === 'json' ? 'default' : 'outline'}
            size='sm'
            onClick={editMode === 'visual' ? switchToJsonMode : undefined}
          >
            JSON
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant='outline' size='sm'>
                <Plus className='mr-1 h-3 w-3' />
                {t('Add Rule')}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem
                onClick={() => {
                  setEditingRule(null)
                  setRuleTemplateKey(null)
                  setRuleEditorOpen(true)
                }}
              >
                {t('Blank Rule')}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  setEditingRule(null)
                  setRuleTemplateKey('codexCli')
                  setRuleEditorOpen(true)
                }}
              >
                Codex CLI
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => {
                  setEditingRule(null)
                  setRuleTemplateKey('claudeCli')
                  setRuleEditorOpen(true)
                }}
              >
                Claude CLI
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
          <Button variant='outline' size='sm' onClick={handleFillTemplates}>
            <FileText className='mr-1 h-3 w-3' />
            {t('Fill Templates')}
          </Button>
          <Button size='sm' onClick={handleSave} disabled={saving}>
            {saving ? t('Saving...') : t('Save')}
          </Button>
          <Button
            variant='outline'
            size='sm'
            onClick={refreshCache}
            disabled={cacheLoading}
          >
            <RefreshCw
              className={`mr-1 h-3 w-3 ${cacheLoading ? 'animate-spin' : ''}`}
            />
            {t('Refresh Cache')}
          </Button>
          <Button
            variant='destructive'
            size='sm'
            onClick={() => setClearAllDialogOpen(true)}
          >
            {t('Clear All Cache')}
          </Button>
          <div className='flex items-center gap-1.5'>
            <Combobox
              className='h-8 w-56'
              options={groupOptions}
              value={groupToClear}
              onValueChange={setGroupToClear}
              allowCustomValue
              placeholder={t('Group to reset, e.g. 2freecodex')}
              searchPlaceholder={t('Search or type a group')}
              emptyText={t('No cached group')}
            />
            <Button
              variant='outline'
              size='sm'
              disabled={!groupToClear.trim()}
              onClick={() => setClearGroupDialogOpen(true)}
            >
              {t('Reset Group Affinity')}
            </Button>
          </div>
          {cacheStats && (
            <span className='text-muted-foreground text-xs'>
              {t('Cache Entries')}: {cacheStats.total} /{' '}
              {cacheStats.cache_capacity}
            </span>
          )}
        </div>

        {/* Rules Table or JSON Editor */}
        {editMode === 'visual' ? (
          <div className='overflow-x-auto rounded-md border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('Name')}</TableHead>
                  <TableHead>{t('Model Regex')}</TableHead>
                  <TableHead>{t('Key Sources')}</TableHead>
                  <TableHead>{t('TTL')}</TableHead>
                  <TableHead>{t('Retry')}</TableHead>
                  <TableHead>{t('Scope')}</TableHead>
                  <TableHead>{t('Cache')}</TableHead>
                  <TableHead className='text-right'>{t('Actions')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className='text-muted-foreground py-8 text-center'
                    >
                      {t('No rules yet')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rules.map((rule, idx) => (
                    <TableRow key={idx}>
                      <TableCell className='font-medium'>
                        {rule.name || '-'}
                      </TableCell>
                      <TableCell>
                        <div className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
                          {(rule.model_regex || []).length > 0 && (
                            <span
                              className='size-1.5 shrink-0 rounded-full bg-slate-400'
                              aria-hidden='true'
                            />
                          )}
                          {(rule.model_regex || [])
                            .slice(0, 2)
                            .map((r, i, arr) => (
                              <span
                                key={i}
                                className='flex items-center gap-1.5'
                              >
                                {r}
                                {i < arr.length - 1 && (
                                  <span className='text-muted-foreground/30'>
                                    ·
                                  </span>
                                )}
                              </span>
                            ))}
                          {(rule.model_regex || []).length > 2 && (
                            <span className='text-muted-foreground/50'>
                              +{(rule.model_regex || []).length - 2}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <div className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
                          {(rule.key_sources || []).length > 0 && (
                            <span
                              className='size-1.5 shrink-0 rounded-full bg-slate-400'
                              aria-hidden='true'
                            />
                          )}
                          {(rule.key_sources || [])
                            .slice(0, 2)
                            .map((src, i, arr) => (
                              <span
                                key={i}
                                className='flex items-center gap-1.5'
                              >
                                {src.type}:
                                {src.type === 'gjson' ? src.path : src.key}
                                {i < arr.length - 1 && (
                                  <span className='text-muted-foreground/30'>
                                    ·
                                  </span>
                                )}
                              </span>
                            ))}
                          {(rule.key_sources || []).length > 2 && (
                            <span className='text-muted-foreground/50'>
                              +{(rule.key_sources || []).length - 2}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>{rule.ttl_seconds || '-'}</TableCell>
                      <TableCell>
                        <StatusBadge
                          label={
                            rule.skip_retry_on_failure
                              ? t('No Retry')
                              : t('Retry')
                          }
                          variant={
                            rule.skip_retry_on_failure ? 'danger' : 'neutral'
                          }
                          copyable={false}
                        />
                      </TableCell>
                      <TableCell>
                        {(() => {
                          const scopeItems = [
                            rule.include_using_group && t('Group'),
                            rule.include_model_name && t('Model'),
                            rule.include_rule_name && t('Rule'),
                          ].filter(Boolean) as string[]
                          if (scopeItems.length === 0) return '-'
                          return (
                            <div className='text-muted-foreground flex items-center gap-1.5 text-xs font-medium'>
                              <span
                                className='size-1.5 shrink-0 rounded-full bg-slate-400'
                                aria-hidden='true'
                              />
                              {scopeItems.map((item, idx, arr) => (
                                <span
                                  key={idx}
                                  className='flex items-center gap-1.5'
                                >
                                  {item}
                                  {idx < arr.length - 1 && (
                                    <span className='text-muted-foreground/30'>
                                      ·
                                    </span>
                                  )}
                                </span>
                              ))}
                            </div>
                          )
                        })()}
                      </TableCell>
                      <TableCell>
                        {rule.include_rule_name && cacheStats?.by_rule_name
                          ? cacheStats.by_rule_name[rule.name] || 0
                          : 'N/A'}
                      </TableCell>
                      <TableCell className='text-right'>
                        <div className='flex justify-end gap-1'>
                          {rule.include_rule_name && (
                            <Button
                              variant='ghost'
                              size='icon'
                              className='h-7 w-7'
                              onClick={() => setClearRuleName(rule.name)}
                              title={t('Clear cache for this rule')}
                            >
                              <X className='h-3 w-3' />
                            </Button>
                          )}
                          <Button
                            variant='ghost'
                            size='icon'
                            className='h-7 w-7'
                            onClick={() => {
                              setEditingRule(rule)
                              setRuleTemplateKey(null)
                              setRuleEditorOpen(true)
                            }}
                          >
                            <Edit className='h-3 w-3' />
                          </Button>
                          <Button
                            variant='ghost'
                            size='icon'
                            className='h-7 w-7'
                            onClick={() => handleDeleteRule(idx)}
                          >
                            <Trash2 className='h-3 w-3' />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        ) : (
          <div className='grid gap-1.5'>
            <Label htmlFor='channel-affinity-rules-json'>
              {t('Rules JSON')}
            </Label>
            <JsonCodeEditor
              id='channel-affinity-rules-json'
              value={jsonText}
              onChange={setJsonText}
              heightClassName='h-[300px] min-h-[300px] max-h-[300px]'
            />
          </div>
        )}
      </SettingsSection>

      <RuleEditorDialog
        open={ruleEditorOpen}
        onOpenChange={setRuleEditorOpen}
        rule={editingRule}
        onSave={handleRuleSave}
        templateKey={ruleTemplateKey}
      />

      <ConfirmDialog
        open={clearAllDialogOpen}
        onOpenChange={setClearAllDialogOpen}
        title={t('Confirm clearing all channel affinity cache')}
        desc={t(
          'This will delete all channel affinity cache entries still in memory.'
        )}
        handleConfirm={handleClearAll}
        destructive
      />

      <ConfirmDialog
        open={clearGroupDialogOpen}
        onOpenChange={setClearGroupDialogOpen}
        title={t('Confirm resetting affinity for this group')}
        desc={`${t('Group')}: ${groupToClear.trim()} — ${t(
          'This will delete all affinity pins scoped to this group across all rules, so its next requests re-select a fresh channel.'
        )}`}
        handleConfirm={handleClearGroup}
        destructive
      />

      {clearRuleName !== null && (
        <ConfirmDialog
          open
          onOpenChange={(v) => !v && setClearRuleName(null)}
          title={t('Confirm clearing cache for this rule')}
          desc={`${t('Rule')}: ${clearRuleName}`}
          handleConfirm={handleClearRule}
          destructive
        />
      )}

      <ConfirmDialog
        open={fillTemplateDialogOpen}
        onOpenChange={setFillTemplateDialogOpen}
        title={t('Fill Codex CLI / Claude CLI Templates')}
        desc={t(
          'This will append 2 template rules (Codex CLI and Claude CLI) to the existing rule list.'
        )}
        handleConfirm={appendCliTemplates}
      />
    </>
  )
}
