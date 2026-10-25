import { useId, useMemo, useState } from 'react'
import { Pencil, Plus, Search, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { safeJsonParse } from '../utils/json-parser'

type ModelRPMRule = {
  global_rpm: number
  group_rpm?: Record<string, number>
}

type ModelRPMConfig = {
  enabled: boolean
  models: Record<string, ModelRPMRule>
}

type DraftRule = {
  modelName: string
  globalRPM: number
  groupRPM: Record<string, number>
}

type ModelRPMVisualEditorProps = {
  value: string
  modelRatio: string
  modelPrice: string
  topupGroupRatio: string
  onChange: (value: string) => void
}

const emptyConfig = (): ModelRPMConfig => ({ enabled: false, models: {} })

function parseConfig(value: string): ModelRPMConfig {
  const parsed = safeJsonParse<Partial<ModelRPMConfig>>(value, {
    fallback: emptyConfig(),
    silent: true,
  })
  return {
    enabled: parsed.enabled === true,
    models: parsed.models ?? {},
  }
}

function parseNumberMap(value: string) {
  return safeJsonParse<Record<string, number>>(value, {
    fallback: {},
    silent: true,
  })
}

export function ModelRPMVisualEditor({
  value,
  modelRatio,
  modelPrice,
  topupGroupRatio,
  onChange,
}: ModelRPMVisualEditorProps) {
  const { t } = useTranslation()
  const modelListId = useId()
  const [searchText, setSearchText] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingModel, setEditingModel] = useState<string | null>(null)
  const [draft, setDraft] = useState<DraftRule>({
    modelName: '',
    globalRPM: 20,
    groupRPM: {},
  })

  const config = useMemo(() => parseConfig(value), [value])
  const modelNames = useMemo(() => {
    const names = new Set([
      ...Object.keys(parseNumberMap(modelRatio)),
      ...Object.keys(parseNumberMap(modelPrice)),
      ...Object.keys(config.models),
    ])
    return [...names].sort((a, b) => a.localeCompare(b))
  }, [config.models, modelPrice, modelRatio])
  const customerTiers = useMemo(() => {
    const names = new Set(Object.keys(parseNumberMap(topupGroupRatio)))
    Object.values(config.models).forEach((rule) => {
      Object.keys(rule.group_rpm ?? {}).forEach((tier) => names.add(tier))
    })
    return [...names].sort((a, b) => a.localeCompare(b))
  }, [config.models, topupGroupRatio])
  const rows = useMemo(() => {
    const search = searchText.trim().toLowerCase()
    return Object.entries(config.models)
      .filter(
        ([modelName]) => !search || modelName.toLowerCase().includes(search)
      )
      .sort(([a], [b]) => a.localeCompare(b))
  }, [config.models, searchText])

  const emit = (next: ModelRPMConfig) => onChange(JSON.stringify(next, null, 2))

  const openAdd = () => {
    setEditingModel(null)
    setDraft({ modelName: '', globalRPM: 20, groupRPM: {} })
    setDialogOpen(true)
  }

  const openEdit = (modelName: string, rule: ModelRPMRule) => {
    setEditingModel(modelName)
    setDraft({
      modelName,
      globalRPM: rule.global_rpm,
      groupRPM: { ...(rule.group_rpm ?? {}) },
    })
    setDialogOpen(true)
  }

  const saveDraft = () => {
    const modelName = draft.modelName.trim()
    if (!modelName || draft.globalRPM < 1) return

    const models = { ...config.models }
    if (editingModel && editingModel !== modelName) delete models[editingModel]
    models[modelName] = {
      global_rpm: draft.globalRPM,
      ...(Object.keys(draft.groupRPM).length > 0
        ? { group_rpm: draft.groupRPM }
        : {}),
    }
    emit({ ...config, models })
    setDialogOpen(false)
  }

  const deleteRule = (modelName: string) => {
    const models = { ...config.models }
    delete models[modelName]
    emit({ ...config, models })
  }

  const toggleTier = (tier: string, checked: boolean) => {
    const groupRPM = { ...draft.groupRPM }
    if (checked) groupRPM[tier] = Math.max(draft.globalRPM, 1)
    else delete groupRPM[tier]
    setDraft({ ...draft, groupRPM })
  }

  return (
    <div className='space-y-4 border-t pt-6'>
      <div className='flex items-center justify-between gap-4'>
        <Label htmlFor='model-rpm-enabled' className='text-base'>
          {t('Per-model RPM limits')}
        </Label>
        <Switch
          id='model-rpm-enabled'
          checked={config.enabled}
          onCheckedChange={(enabled) => emit({ ...config, enabled })}
        />
      </div>

      {config.enabled && (
        <>
          <div className='flex flex-col gap-3 sm:flex-row'>
            <div className='relative flex-1'>
              <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-4 w-4' />
              <Input
                placeholder={t('Search models...')}
                value={searchText}
                onChange={(event) => setSearchText(event.target.value)}
                className='pl-9'
              />
            </div>
            <Button type='button' onClick={openAdd}>
              <Plus className='h-4 w-4' />
              {t('Add model limit')}
            </Button>
          </div>

          {rows.length === 0 ? (
            <div className='text-muted-foreground rounded-lg border border-dashed p-8 text-center text-sm'>
              {t('No model RPM limits configured')}
            </div>
          ) : (
            <div className='overflow-x-auto rounded-md border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Model')}</TableHead>
                    <TableHead className='text-right'>
                      {t('Default RPM')}
                    </TableHead>
                    <TableHead>{t('Customer tier overrides')}</TableHead>
                    <TableHead className='w-24 text-right'>
                      {t('Actions')}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map(([modelName, rule]) => (
                    <TableRow key={modelName}>
                      <TableCell className='font-mono font-medium'>
                        {modelName}
                      </TableCell>
                      <TableCell className='text-right font-mono'>
                        {rule.global_rpm}
                      </TableCell>
                      <TableCell className='text-muted-foreground text-sm'>
                        {Object.entries(rule.group_rpm ?? {}).length > 0
                          ? Object.entries(rule.group_rpm ?? {})
                              .sort(([a], [b]) => a.localeCompare(b))
                              .map(([tier, rpm]) => `${tier}: ${rpm}`)
                              .join(', ')
                          : t('None')}
                      </TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-1'>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            title={t('Edit')}
                            aria-label={t('Edit')}
                            onClick={() => openEdit(modelName, rule)}
                          >
                            <Pencil className='h-4 w-4' />
                          </Button>
                          <Button
                            type='button'
                            variant='ghost'
                            size='icon'
                            title={t('Delete')}
                            aria-label={t('Delete')}
                            onClick={() => deleteRule(modelName)}
                          >
                            <Trash2 className='h-4 w-4' />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </>
      )}

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className='sm:max-w-[640px]'>
          <DialogHeader>
            <DialogTitle>
              {editingModel ? t('Edit model limit') : t('Add model limit')}
            </DialogTitle>
          </DialogHeader>

          <div className='space-y-5 overflow-y-auto pr-1 sm:max-h-[60vh]'>
            <div className='space-y-2'>
              <Label htmlFor='model-rpm-name'>{t('Model')}</Label>
              <Input
                id='model-rpm-name'
                list={modelListId}
                value={draft.modelName}
                onChange={(event) =>
                  setDraft({ ...draft, modelName: event.target.value })
                }
              />
              <datalist id={modelListId}>
                {modelNames.map((modelName) => (
                  <option key={modelName} value={modelName} />
                ))}
              </datalist>
            </div>

            <div className='space-y-2'>
              <Label htmlFor='model-rpm-default'>{t('Default RPM')}</Label>
              <Input
                id='model-rpm-default'
                type='number'
                min={1}
                step={1}
                value={draft.globalRPM}
                onChange={(event) =>
                  setDraft({ ...draft, globalRPM: Number(event.target.value) })
                }
              />
            </div>

            {customerTiers.length > 0 && (
              <div className='space-y-2'>
                <Label>{t('Customer tier overrides')}</Label>
                <div className='divide-y rounded-md border'>
                  {customerTiers.map((tier) => {
                    const enabled = tier in draft.groupRPM
                    return (
                      <div
                        key={tier}
                        className='grid min-h-12 grid-cols-[1fr_9rem] items-center gap-4 px-3 py-2'
                      >
                        <label className='flex min-w-0 items-center gap-3'>
                          <Checkbox
                            checked={enabled}
                            onCheckedChange={(checked) =>
                              toggleTier(tier, checked === true)
                            }
                          />
                          <span className='truncate font-medium'>{tier}</span>
                        </label>
                        <Input
                          type='number'
                          min={1}
                          step={1}
                          disabled={!enabled}
                          value={enabled ? draft.groupRPM[tier] : ''}
                          aria-label={`${tier} RPM`}
                          onChange={(event) =>
                            setDraft({
                              ...draft,
                              groupRPM: {
                                ...draft.groupRPM,
                                [tier]: Number(event.target.value),
                              },
                            })
                          }
                        />
                      </div>
                    )
                  })}
                </div>
              </div>
            )}
          </div>

          <DialogFooter>
            <Button
              type='button'
              variant='outline'
              onClick={() => setDialogOpen(false)}
            >
              {t('Cancel')}
            </Button>
            <Button
              type='button'
              disabled={
                !draft.modelName.trim() ||
                draft.globalRPM < 1 ||
                Object.values(draft.groupRPM).some((rpm) => rpm < 1)
              }
              onClick={saveDraft}
            >
              {editingModel ? t('Update') : t('Add')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
