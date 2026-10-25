import { useMemo, useState } from 'react'
import { ArrowDown, ArrowUp, Check, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
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
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { JsonCodeEditor, formatJsonDraft } from '@/components/json-code-editor'
import { REQUEST_ENDPOINT_TYPE_OPTIONS } from '../../constants'
import {
  ADVANCED_CUSTOM_BALANCE_PATH,
  ADVANCED_CUSTOM_CONVERTER_OPTIONS,
  ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS,
  ADVANCED_CUSTOM_MODEL_LIST_PATH,
  getAdvancedCustomConverterUpstreamPath,
  getAdvancedCustomEndpointType,
  getAdvancedCustomManagementRoute,
  isAdvancedCustomConverterPathAllowed,
  isAdvancedCustomManagementPath,
  parseAdvancedCustomConfig,
  stringifyAdvancedCustomConfig,
  validateAdvancedCustomConfig,
  type AdvancedCustomConfig,
  type AdvancedCustomConverter,
  type AdvancedCustomRoute,
} from '../../lib/advanced-custom'

type AdvancedCustomEditorDialogProps = {
  open: boolean
  value: string
  onOpenChange: (open: boolean) => void
  onSave: (value: string) => void
}

type AuthMode = 'default' | 'none' | 'header' | 'query'

// Visual-editor row: models are edited as free text so commas can be typed.
type RouteDraft = {
  key: number
  incoming_path: string
  upstream_path: string
  converter: string
  modelsText: string
  authMode: AuthMode
  authName: string
  authValue: string
  passThroughBody: boolean
}

type VisualState = {
  routes: RouteDraft[]
  modelListPath: string
  balancePath: string
}

const CUSTOM_INCOMING = '__custom__'
const DEFAULT_AUTH = { name: 'Authorization', value: 'Bearer {api_key}' }

let routeKeySeed = 0
const nextRouteKey = () => ++routeKeySeed

function toDraft(route: AdvancedCustomRoute): RouteDraft {
  const authType = route.auth?.type?.trim()
  const authMode: AuthMode =
    !route.auth || !authType
      ? 'default'
      : authType === 'none' || authType === 'header' || authType === 'query'
        ? authType
        : 'header'
  return {
    key: nextRouteKey(),
    incoming_path: route.incoming_path,
    upstream_path: route.upstream_path,
    converter: route.converter || 'none',
    modelsText: (route.models || []).join(', '),
    authMode,
    authName: route.auth?.name || '',
    authValue: route.auth?.value || '',
    passThroughBody: route.pass_through_body_enabled === true,
  }
}

function fromDraft(draft: RouteDraft): AdvancedCustomRoute {
  const route: AdvancedCustomRoute = {
    incoming_path: draft.incoming_path.trim(),
    upstream_path: draft.upstream_path.trim(),
    converter: draft.converter || 'none',
    models: draft.modelsText
      .split(/[,\n]/)
      .map((model) => model.trim())
      .filter(Boolean),
  }
  if (draft.passThroughBody && route.converter === 'none')
    route.pass_through_body_enabled = true
  if (draft.authMode === 'none') route.auth = { type: 'none' }
  if (draft.authMode === 'header' || draft.authMode === 'query') {
    route.auth = {
      type: draft.authMode,
      name: draft.authName,
      value: draft.authValue,
    }
  }
  return route
}

function managementRoute(path: string, upstream: string): AdvancedCustomRoute {
  return {
    incoming_path: path,
    upstream_path: upstream.trim(),
    converter: 'none',
    models: [],
  }
}

function toVisualState(config: AdvancedCustomConfig): VisualState {
  return {
    routes: config.advanced_routes
      .filter((route) => !isAdvancedCustomManagementPath(route.incoming_path))
      .map(toDraft),
    modelListPath:
      getAdvancedCustomManagementRoute(config, ADVANCED_CUSTOM_MODEL_LIST_PATH)
        ?.upstream_path || '',
    balancePath:
      getAdvancedCustomManagementRoute(config, ADVANCED_CUSTOM_BALANCE_PATH)
        ?.upstream_path || '',
  }
}

function fromVisualState(
  state: VisualState,
  previous: AdvancedCustomConfig | null
): AdvancedCustomConfig {
  const routes = state.routes.map(fromDraft)
  // Keep any auth configured on management routes through JSON.
  for (const [path, upstream] of [
    [ADVANCED_CUSTOM_MODEL_LIST_PATH, state.modelListPath],
    [ADVANCED_CUSTOM_BALANCE_PATH, state.balancePath],
  ] as const) {
    if (!upstream.trim()) continue
    const existing = previous
      ? getAdvancedCustomManagementRoute(previous, path)
      : undefined
    routes.push({ ...managementRoute(path, upstream), auth: existing?.auth })
  }
  return { advanced_routes: routes }
}

function newRouteDraft(incomingPath = '/v1/chat/completions'): RouteDraft {
  return {
    key: nextRouteKey(),
    incoming_path: incomingPath,
    upstream_path: incomingPath,
    converter: 'none',
    modelsText: '',
    authMode: 'default',
    authName: DEFAULT_AUTH.name,
    authValue: DEFAULT_AUTH.value,
    passThroughBody: false,
  }
}

function incomingPathLabel(path: string): string {
  const endpoint = getAdvancedCustomEndpointType(path)
  const label = REQUEST_ENDPOINT_TYPE_OPTIONS.find(
    (option) => option.value === endpoint
  )?.label
  if (!label) return path
  if (path.includes(':streamGenerateContent')) return `${label} (stream)`
  return label
}

export function AdvancedCustomEditorDialog({
  open,
  value,
  onOpenChange,
  onSave,
}: AdvancedCustomEditorDialogProps) {
  const { t } = useTranslation()
  // The caller mounts this dialog only while it is open, so each opening gets
  // a fresh state without a prop-to-state synchronization effect.
  const [initialConfig] = useState(() => parseAdvancedCustomConfig(value))
  const [mode, setMode] = useState<'visual' | 'json'>(
    initialConfig ? 'visual' : 'json'
  )
  const [visual, setVisual] = useState<VisualState>(() =>
    toVisualState(initialConfig || { advanced_routes: [] })
  )
  const [jsonText, setJsonText] = useState(() =>
    initialConfig ? stringifyAdvancedCustomConfig(initialConfig) : value
  )
  const [lastConfig, setLastConfig] = useState(initialConfig)

  const currentConfig = useMemo(
    () =>
      mode === 'visual'
        ? fromVisualState(visual, lastConfig)
        : parseAdvancedCustomConfig(jsonText),
    [mode, visual, jsonText, lastConfig]
  )
  // Removing every route clears the setting and restores legacy Custom.
  const isEmpty = currentConfig?.advanced_routes.length === 0
  const validationError = useMemo(
    () =>
      currentConfig
        ? isEmpty
          ? null
          : validateAdvancedCustomConfig(currentConfig)
        : 'Invalid JSON',
    [currentConfig, isEmpty]
  )

  const switchMode = (next: string) => {
    if (next === mode) return
    if (next === 'json') {
      const config = fromVisualState(visual, lastConfig)
      setJsonText(stringifyAdvancedCustomConfig(config))
      setLastConfig(config)
      setMode('json')
      return
    }
    const parsed = parseAdvancedCustomConfig(jsonText)
    if (!parsed) {
      toast.error(t('Fix the JSON before switching to the visual editor'))
      return
    }
    setVisual(toVisualState(parsed))
    setLastConfig(parsed)
    setMode('visual')
  }

  const updateRoute = (key: number, patch: Partial<RouteDraft>) => {
    setVisual((state) => ({
      ...state,
      routes: state.routes.map((route) => {
        if (route.key !== key) return route
        const next = { ...route, ...patch }
        // Follow the protocol default while the upstream path was untouched.
        const previousDefault = getAdvancedCustomConverterUpstreamPath(
          route.converter,
          route.incoming_path
        )
        if (
          (patch.incoming_path !== undefined ||
            patch.converter !== undefined) &&
          route.upstream_path.trim() === previousDefault
        ) {
          next.upstream_path = getAdvancedCustomConverterUpstreamPath(
            next.converter,
            next.incoming_path
          )
        }
        return next
      }),
    }))
  }

  const changeIncoming = (route: RouteDraft, incomingPath: string) => {
    const converterAllowed = isAdvancedCustomConverterPathAllowed(
      incomingPath,
      route.converter as AdvancedCustomConverter
    )
    updateRoute(route.key, {
      incoming_path: incomingPath,
      ...(converterAllowed ? {} : { converter: 'none' }),
    })
  }

  const moveRoute = (index: number, delta: number) => {
    setVisual((state) => {
      const target = index + delta
      if (target < 0 || target >= state.routes.length) return state
      const routes = [...state.routes]
      ;[routes[index], routes[target]] = [routes[target], routes[index]]
      return { ...state, routes }
    })
  }

  const removeRoute = (key: number) => {
    setVisual((state) => ({
      ...state,
      routes: state.routes.filter((route) => route.key !== key),
    }))
  }

  const addRoute = () => {
    setVisual((state) => ({
      ...state,
      routes: [...state.routes, newRouteDraft()],
    }))
  }

  const save = () => {
    if (!currentConfig || validationError) {
      toast.error(t(validationError || 'Invalid JSON'))
      return
    }
    onSave(isEmpty ? '' : stringifyAdvancedCustomConfig(currentConfig))
    onOpenChange(false)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className='flex max-h-[90vh] flex-col sm:max-w-4xl'>
        <DialogHeader>
          <DialogTitle>{t('Advanced Custom Routes')}</DialogTitle>
          <DialogDescription>
            {t(
              'Each route declares an inbound endpoint this channel accepts and how it is forwarded upstream. Requests for endpoints without a route are sent to other channels.'
            )}
          </DialogDescription>
        </DialogHeader>

        <Tabs
          value={mode}
          onValueChange={switchMode}
          className='flex min-h-0 flex-1 flex-col'
        >
          <TabsList className='self-start'>
            <TabsTrigger value='visual'>{t('Visual editor')}</TabsTrigger>
            <TabsTrigger value='json'>JSON</TabsTrigger>
          </TabsList>

          <TabsContent
            value='visual'
            className='min-h-0 flex-1 space-y-4 overflow-y-auto pr-1'
          >
            <div className='space-y-3'>
              {visual.routes.length === 0 && (
                <p className='text-muted-foreground rounded-lg border border-dashed p-4 text-center text-sm'>
                  {t(
                    'No relay routes yet. Without routes this Custom channel forwards everything to its Base URL.'
                  )}
                </p>
              )}
              {visual.routes.map((route, index) => (
                <RouteCard
                  key={route.key}
                  route={route}
                  index={index}
                  total={visual.routes.length}
                  onIncomingChange={(path) => changeIncoming(route, path)}
                  onChange={(patch) => updateRoute(route.key, patch)}
                  onMove={(delta) => moveRoute(index, delta)}
                  onRemove={() => removeRoute(route.key)}
                />
              ))}
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={addRoute}
              >
                <Plus className='mr-1.5 size-4' aria-hidden='true' />
                {t('Add route')}
              </Button>
            </div>

            <div className='space-y-3 rounded-lg border p-4'>
              <div>
                <div className='text-sm font-medium'>
                  {t('Management endpoints')}
                </div>
                <p className='text-muted-foreground text-xs'>
                  {t(
                    'Optional. Used for fetching the model list and querying balance; they do not accept relay requests.'
                  )}
                </p>
              </div>
              <div className='grid gap-3 sm:grid-cols-2'>
                <div className='space-y-1.5'>
                  <Label htmlFor='advanced-custom-models-path'>
                    {t('Model list upstream path')}
                  </Label>
                  <Input
                    id='advanced-custom-models-path'
                    value={visual.modelListPath}
                    placeholder='/provider/v1/models'
                    onChange={(event) =>
                      setVisual((state) => ({
                        ...state,
                        modelListPath: event.target.value,
                      }))
                    }
                  />
                </div>
                <div className='space-y-1.5'>
                  <Label htmlFor='advanced-custom-balance-path'>
                    {t('Balance upstream path')}
                  </Label>
                  <Input
                    id='advanced-custom-balance-path'
                    value={visual.balancePath}
                    placeholder='/provider/balance'
                    onChange={(event) =>
                      setVisual((state) => ({
                        ...state,
                        balancePath: event.target.value,
                      }))
                    }
                  />
                </div>
              </div>
            </div>
          </TabsContent>

          <TabsContent value='json' className='min-h-0 flex-1 space-y-2'>
            <div className='flex justify-end'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                disabled={!jsonText.trim()}
                onClick={() => {
                  const formatted = formatJsonDraft(jsonText)
                  if (formatted) setJsonText(formatted)
                }}
              >
                {t('Format JSON')}
              </Button>
            </div>
            <JsonCodeEditor
              value={jsonText}
              onChange={setJsonText}
              heightClassName='h-[min(48vh,30rem)] min-h-[20rem]'
              ariaLabel={t('Advanced Custom routes JSON')}
              aria-invalid={Boolean(validationError)}
            />
            <p className='text-muted-foreground text-xs'>
              {t(
                'Management routes use exact incoming paths and never accept model placeholders.'
              )}
            </p>
          </TabsContent>
        </Tabs>

        {validationError && (
          <p className='text-destructive text-xs'>{t(validationError)}</p>
        )}

        <DialogFooter>
          <Button
            type='button'
            variant='outline'
            onClick={() => onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button
            type='button'
            onClick={save}
            disabled={!currentConfig || Boolean(validationError)}
          >
            <Check className='mr-1.5 size-4' aria-hidden='true' />
            {t('Save changes')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

type RouteCardProps = {
  route: RouteDraft
  index: number
  total: number
  onIncomingChange: (path: string) => void
  onChange: (patch: Partial<RouteDraft>) => void
  onMove: (delta: number) => void
  onRemove: () => void
}

function RouteCard({
  route,
  index,
  total,
  onIncomingChange,
  onChange,
  onMove,
  onRemove,
}: RouteCardProps) {
  const { t } = useTranslation()
  const isPreset = ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS.includes(
    route.incoming_path
  )
  const [customIncoming, setCustomIncoming] = useState(!isPreset)
  const converterOptions = ADVANCED_CUSTOM_CONVERTER_OPTIONS.filter((option) =>
    isAdvancedCustomConverterPathAllowed(route.incoming_path, option.value)
  )
  const id = `advanced-custom-route-${route.key}`

  return (
    <div className='bg-card space-y-3 rounded-lg border p-4'>
      <div className='flex items-center justify-between gap-2'>
        <div className='text-sm font-medium'>
          {t('Route')} {index + 1}
          <span className='text-muted-foreground ml-2 font-mono text-xs'>
            {route.incoming_path}
          </span>
        </div>
        <div className='flex gap-1'>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='size-7'
            disabled={index === 0}
            aria-label={t('Move up')}
            onClick={() => onMove(-1)}
          >
            <ArrowUp className='size-4' aria-hidden='true' />
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='size-7'
            disabled={index === total - 1}
            aria-label={t('Move down')}
            onClick={() => onMove(1)}
          >
            <ArrowDown className='size-4' aria-hidden='true' />
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='icon'
            className='text-destructive size-7'
            aria-label={t('Delete route')}
            onClick={onRemove}
          >
            <Trash2 className='size-4' aria-hidden='true' />
          </Button>
        </div>
      </div>

      <div className='grid gap-3 sm:grid-cols-2'>
        <div className='space-y-1.5'>
          <Label htmlFor={`${id}-incoming`}>{t('Inbound endpoint')}</Label>
          <Select
            value={customIncoming ? CUSTOM_INCOMING : route.incoming_path}
            onValueChange={(next) => {
              if (next === CUSTOM_INCOMING) {
                setCustomIncoming(true)
                return
              }
              setCustomIncoming(false)
              onIncomingChange(next)
            }}
          >
            <SelectTrigger id={`${id}-incoming`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ADVANCED_CUSTOM_INCOMING_PATH_OPTIONS.map((path) => (
                <SelectItem key={path} value={path}>
                  {t(incomingPathLabel(path))}
                  <span className='text-muted-foreground ml-2 font-mono text-xs'>
                    {path}
                  </span>
                </SelectItem>
              ))}
              <SelectItem value={CUSTOM_INCOMING}>{t('Other path')}</SelectItem>
            </SelectContent>
          </Select>
          {customIncoming && (
            <Input
              value={route.incoming_path}
              placeholder='/v1/...'
              aria-label={t('Inbound path')}
              onChange={(event) => onIncomingChange(event.target.value)}
            />
          )}
        </div>

        <div className='space-y-1.5'>
          <Label htmlFor={`${id}-converter`}>{t('Conversion')}</Label>
          <Select
            value={route.converter}
            onValueChange={(next) => onChange({ converter: next })}
          >
            <SelectTrigger id={`${id}-converter`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {converterOptions.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {t(option.label)}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        {route.converter === 'none' && (
          <div className='flex items-start justify-between gap-3 rounded-md border p-3 sm:col-span-2'>
            <div className='space-y-0.5'>
              <Label htmlFor={`${id}-passthrough`}>
                {t('Pass through request body')}
              </Label>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'Forward the client request body unchanged for this route. Model mapping, parameter overrides and field filtering are skipped.'
                )}
              </p>
            </div>
            <Switch
              id={`${id}-passthrough`}
              checked={route.passThroughBody}
              onCheckedChange={(checked) =>
                onChange({ passThroughBody: checked })
              }
            />
          </div>
        )}

        <div className='space-y-1.5 sm:col-span-2'>
          <Label htmlFor={`${id}-upstream`}>{t('Upstream path')}</Label>
          <Input
            id={`${id}-upstream`}
            value={route.upstream_path}
            placeholder='/v1/chat/completions'
            onChange={(event) =>
              onChange({ upstream_path: event.target.value })
            }
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'A path joined to the Base URL, or a full URL. {model} is replaced with the upstream model name.'
            )}
          </p>
        </div>

        <div className='space-y-1.5 sm:col-span-2'>
          <Label htmlFor={`${id}-models`}>{t('Models')}</Label>
          <Input
            id={`${id}-models`}
            value={route.modelsText}
            placeholder={t('Empty = all models of this channel')}
            onChange={(event) => onChange({ modelsText: event.target.value })}
          />
          <p className='text-muted-foreground text-xs'>
            {t(
              'Comma separated; prefix re: for a regular expression. A route for all models must come after model-specific routes of the same endpoint.'
            )}
          </p>
        </div>

        <div className='space-y-1.5'>
          <Label htmlFor={`${id}-auth`}>{t('Authentication')}</Label>
          <Select
            value={route.authMode}
            onValueChange={(next) => {
              const authMode = next as AuthMode
              onChange({
                authMode,
                ...((authMode === 'header' || authMode === 'query') &&
                !route.authName.trim()
                  ? {
                      authName:
                        authMode === 'header' ? DEFAULT_AUTH.name : 'key',
                      authValue:
                        authMode === 'header'
                          ? DEFAULT_AUTH.value
                          : '{api_key}',
                    }
                  : {}),
              })
            }}
          >
            <SelectTrigger id={`${id}-auth`} className='w-full'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value='default'>
                {t('Default (Authorization: Bearer key)')}
              </SelectItem>
              <SelectItem value='header'>{t('Custom header')}</SelectItem>
              <SelectItem value='query'>{t('Query parameter')}</SelectItem>
              <SelectItem value='none'>{t('No authentication')}</SelectItem>
            </SelectContent>
          </Select>
        </div>

        {(route.authMode === 'header' || route.authMode === 'query') && (
          <div className='grid grid-cols-2 gap-2'>
            <div className='space-y-1.5'>
              <Label htmlFor={`${id}-auth-name`}>{t('Name')}</Label>
              <Input
                id={`${id}-auth-name`}
                value={route.authName}
                onChange={(event) => onChange({ authName: event.target.value })}
              />
            </div>
            <div className='space-y-1.5'>
              <Label htmlFor={`${id}-auth-value`}>{t('Value')}</Label>
              <Input
                id={`${id}-auth-value`}
                value={route.authValue}
                placeholder='Bearer {api_key}'
                onChange={(event) =>
                  onChange({ authValue: event.target.value })
                }
              />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
