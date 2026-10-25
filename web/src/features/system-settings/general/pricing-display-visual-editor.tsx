import { useMemo, useState } from 'react'
import { Code2, Plus, Table2, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import {
  isObjectJson,
  parseEndpointOverrideConfig,
  parseModelContextConfig,
  serializeEndpointOverrideConfig,
  serializeModelContextConfig,
  type ModelContextRow,
  type PricingEndpointRow,
} from './pricing-display-config'

type VisualEditorProps = {
  value: string
  onChange: (value: string) => void
}

const endpointTypes = [
  'openai',
  'openai-response',
  'anthropic',
  'gemini',
  'jina-rerank',
  'image-generation',
  'embeddings',
  'openai-video',
]

const endpointMethods = ['POST', 'GET', 'PUT', 'PATCH', 'DELETE']

function EndpointOverrideEditor({ value, onChange }: VisualEditorProps) {
  const { t } = useTranslation()
  const [rawMode, setRawMode] = useState(false)
  const [rawDraft, setRawDraft] = useState(value)
  const [rawError, setRawError] = useState<string | null>(null)
  const config = useMemo(() => parseEndpointOverrideConfig(value), [value])
  const fallback = config.fallback
  const rows = config.rows

  const updateFallback = (patch: Partial<PricingEndpointRow>) => {
    const nextFallback = { ...fallback, ...patch, group: 'default' }
    onChange(serializeEndpointOverrideConfig(nextFallback, rows))
  }

  const updateRow = (index: number, patch: Partial<PricingEndpointRow>) => {
    const nextRows = rows.map((row, rowIndex) =>
      rowIndex === index ? { ...row, ...patch } : row
    )
    onChange(serializeEndpointOverrideConfig(fallback, nextRows))
  }

  const addRow = () => {
    const nextRows = [
      ...rows,
      {
        group: 'new_group',
        type: 'openai',
        path: '/v1/chat/completions',
        method: 'POST',
      },
    ]
    onChange(serializeEndpointOverrideConfig(fallback, nextRows))
  }

  const removeRow = (index: number) => {
    const nextRows = rows.filter((_, rowIndex) => rowIndex !== index)
    onChange(serializeEndpointOverrideConfig(fallback, nextRows))
  }

  const enterRawMode = () => {
    setRawDraft(value)
    setRawError(null)
    setRawMode(true)
  }

  const exitRawMode = () => {
    if (!isObjectJson(rawDraft)) {
      setRawError(t('Invalid JSON object'))
      return
    }
    setRawError(null)
    setRawMode(false)
    onChange(JSON.stringify(JSON.parse(rawDraft), null, 2))
  }

  if (rawMode) {
    return (
      <div className='space-y-2'>
        <div className='flex items-center justify-between'>
          <Label className='text-sm font-medium'>{t('Raw JSON')}</Label>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={exitRawMode}
          >
            <Table2 className='mr-1.5 size-4' />
            {t('Back to visual editor')}
          </Button>
        </div>
        <Textarea
          value={rawDraft}
          onChange={(event) => {
            setRawDraft(event.target.value)
            setRawError(null)
            if (isObjectJson(event.target.value)) onChange(event.target.value)
          }}
          rows={10}
          spellCheck={false}
          className='font-mono text-xs'
        />
        {rawError && <p className='text-destructive text-xs'>{rawError}</p>}
      </div>
    )
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>
          {t('Visual editor')}
        </span>
        <div className='flex gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={enterRawMode}
          >
            <Code2 className='mr-1.5 size-4' />
            {t('Edit JSON')}
          </Button>
          <Button type='button' variant='outline' size='sm' onClick={addRow}>
            <Plus className='mr-1.5 size-4' />
            {t('Add endpoint override')}
          </Button>
        </div>
      </div>
      <div className='bg-muted/20 space-y-3 rounded-md border p-3'>
        <div className='space-y-0.5'>
          <p className='text-sm font-medium'>
            {t('Default endpoint (Fallback)')}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Used when a group has no dedicated endpoint override. The built-in fallback is OpenAI POST /v1/chat/completions.'
            )}
          </p>
        </div>
        <div className='grid gap-3 md:grid-cols-[minmax(10rem,0.8fr)_minmax(16rem,2fr)_minmax(8rem,0.6fr)]'>
          <div className='space-y-1.5'>
            <Label htmlFor='fallback-endpoint-type'>{t('Endpoint Type')}</Label>
            <Select
              value={fallback.type || 'openai'}
              onValueChange={(type) => updateFallback({ type })}
            >
              <SelectTrigger id='fallback-endpoint-type' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {endpointTypes.map((type) => (
                  <SelectItem key={type} value={type}>
                    {type}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='fallback-endpoint-path'>{t('Path')}</Label>
            <Input
              id='fallback-endpoint-path'
              value={fallback.path}
              onChange={(event) => updateFallback({ path: event.target.value })}
              placeholder='/v1/chat/completions'
              className='font-mono text-xs'
            />
          </div>
          <div className='space-y-1.5'>
            <Label htmlFor='fallback-endpoint-method'>{t('HTTP Method')}</Label>
            <Select
              value={fallback.method || 'POST'}
              onValueChange={(method) => updateFallback({ method })}
            >
              <SelectTrigger id='fallback-endpoint-method' className='w-full'>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {endpointMethods.map((method) => (
                  <SelectItem key={method} value={method}>
                    {method}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>
      </div>
      <p className='text-muted-foreground text-xs font-medium'>
        {t('Group-specific endpoint overrides')}
      </p>
      {rows.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {t('No group-specific endpoint overrides configured')}
        </div>
      ) : (
        <div className='overflow-x-auto rounded-lg border'>
          <Table className='min-w-[760px]'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Group')}</TableHead>
                <TableHead>{t('Endpoint Type')}</TableHead>
                <TableHead>{t('Path')}</TableHead>
                <TableHead>{t('HTTP Method')}</TableHead>
                <TableHead className='w-12 text-right'>
                  {t('Actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, index) => (
                <TableRow key={`${row.group}-${index}`}>
                  <TableCell>
                    <Input
                      value={row.group}
                      onChange={(event) => {
                        if (event.target.value.trim() === 'default') return
                        updateRow(index, { group: event.target.value })
                      }}
                      placeholder='group_name'
                      className='min-w-36'
                    />
                  </TableCell>
                  <TableCell>
                    <Select
                      value={row.type || 'openai'}
                      onValueChange={(type) => updateRow(index, { type })}
                    >
                      <SelectTrigger className='min-w-40'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {endpointTypes.map((type) => (
                          <SelectItem key={type} value={type}>
                            {type}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell>
                    <Input
                      value={row.path}
                      onChange={(event) =>
                        updateRow(index, { path: event.target.value })
                      }
                      placeholder='/v1/chat/completions'
                      className='min-w-64 font-mono text-xs'
                    />
                  </TableCell>
                  <TableCell>
                    <Select
                      value={row.method || 'POST'}
                      onValueChange={(method) => updateRow(index, { method })}
                    >
                      <SelectTrigger className='min-w-28'>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {endpointMethods.map((method) => (
                          <SelectItem key={method} value={method}>
                            {method}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </TableCell>
                  <TableCell className='text-right'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      onClick={() => removeRow(index)}
                      title={t('Remove endpoint override')}
                      aria-label={t('Remove endpoint override')}
                    >
                      <Trash2 className='text-destructive size-4' />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

export function ModelContextVisualEditor({
  value,
  onChange,
}: VisualEditorProps) {
  const { t } = useTranslation()
  const [rawMode, setRawMode] = useState(false)
  const [rawDraft, setRawDraft] = useState(value)
  const [rawError, setRawError] = useState<string | null>(null)
  const config = useMemo(() => parseModelContextConfig(value), [value])
  const fallbackTokens = config.fallbackTokens
  const rows = config.rows

  const updateFallbackTokens = (tokens: number | null) => {
    onChange(serializeModelContextConfig(tokens, rows))
  }

  const updateRow = (index: number, patch: Partial<ModelContextRow>) => {
    const nextRows = rows.map((row, rowIndex) =>
      rowIndex === index ? { ...row, ...patch } : row
    )
    onChange(serializeModelContextConfig(fallbackTokens, nextRows))
  }

  const addRow = () => {
    const nextRows = [...rows, { model: 'new-model', tokens: 128000 }]
    onChange(serializeModelContextConfig(fallbackTokens, nextRows))
  }

  const removeRow = (index: number) => {
    const nextRows = rows.filter((_, rowIndex) => rowIndex !== index)
    onChange(serializeModelContextConfig(fallbackTokens, nextRows))
  }

  const enterRawMode = () => {
    setRawDraft(value)
    setRawError(null)
    setRawMode(true)
  }

  const exitRawMode = () => {
    if (!isObjectJson(rawDraft)) {
      setRawError(t('Invalid JSON object'))
      return
    }
    setRawError(null)
    setRawMode(false)
    onChange(JSON.stringify(JSON.parse(rawDraft), null, 2))
  }

  if (rawMode) {
    return (
      <div className='space-y-2'>
        <div className='flex items-center justify-between'>
          <Label className='text-sm font-medium'>{t('Raw JSON')}</Label>
          <Button
            type='button'
            size='sm'
            variant='outline'
            onClick={exitRawMode}
          >
            <Table2 className='mr-1.5 size-4' />
            {t('Back to visual editor')}
          </Button>
        </div>
        <Textarea
          value={rawDraft}
          onChange={(event) => {
            setRawDraft(event.target.value)
            setRawError(null)
            if (isObjectJson(event.target.value)) onChange(event.target.value)
          }}
          rows={10}
          spellCheck={false}
          className='font-mono text-xs'
        />
        {rawError && <p className='text-destructive text-xs'>{rawError}</p>}
      </div>
    )
  }

  return (
    <div className='space-y-3'>
      <div className='flex items-center justify-between gap-2'>
        <span className='text-muted-foreground text-xs'>
          {t('Visual editor')}
        </span>
        <div className='flex gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={enterRawMode}
          >
            <Code2 className='mr-1.5 size-4' />
            {t('Edit JSON')}
          </Button>
          <Button type='button' variant='outline' size='sm' onClick={addRow}>
            <Plus className='mr-1.5 size-4' />
            {t('Add model context')}
          </Button>
        </div>
      </div>
      <div className='bg-muted/20 space-y-3 rounded-md border p-3'>
        <div className='space-y-0.5'>
          <p className='text-sm font-medium'>
            {t('Default context window (Fallback)')}
          </p>
          <p className='text-muted-foreground text-xs'>
            {t(
              'Used when a model has no dedicated context window. Leave empty to hide context information for unmatched models.'
            )}
          </p>
        </div>
        <div className='max-w-xs space-y-1.5'>
          <Label htmlFor='fallback-context-tokens'>{t('Context tokens')}</Label>
          <Input
            id='fallback-context-tokens'
            type='number'
            min={1}
            step={1}
            value={fallbackTokens ?? ''}
            onChange={(event) =>
              updateFallbackTokens(
                event.target.value ? Number(event.target.value) : null
              )
            }
            placeholder='262144'
            className='font-mono'
          />
        </div>
      </div>
      <p className='text-muted-foreground text-xs font-medium'>
        {t('Model-specific context windows')}
      </p>
      {rows.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-6 text-center text-sm'>
          {t('No model-specific context windows configured')}
        </div>
      ) : (
        <div className='overflow-x-auto rounded-lg border'>
          <Table className='min-w-[560px]'>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Model name')}</TableHead>
                <TableHead>{t('Context tokens')}</TableHead>
                <TableHead className='w-12 text-right'>
                  {t('Actions')}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row, index) => (
                <TableRow key={`${row.model}-${index}`}>
                  <TableCell>
                    <Input
                      value={row.model}
                      onChange={(event) => {
                        if (event.target.value.trim() === 'default') return
                        updateRow(index, { model: event.target.value })
                      }}
                      placeholder='model-name'
                      className='min-w-64'
                    />
                  </TableCell>
                  <TableCell>
                    <Input
                      type='number'
                      min={1}
                      step={1}
                      value={row.tokens || ''}
                      onChange={(event) =>
                        updateRow(index, {
                          tokens: Number(event.target.value) || 0,
                        })
                      }
                      className='min-w-40 font-mono'
                    />
                  </TableCell>
                  <TableCell className='text-right'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='icon'
                      onClick={() => removeRow(index)}
                      title={t('Remove model context')}
                      aria-label={t('Remove model context')}
                    >
                      <Trash2 className='text-destructive size-4' />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

export { EndpointOverrideEditor }
