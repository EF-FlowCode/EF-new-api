import { useState, useMemo, memo, useCallback, useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import {
  type ColumnDef,
  type PaginationState,
  type VisibilityState,
  type SortingState,
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  DataTableColumnHeader,
  DataTableToolbar,
  DataTablePagination,
} from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { getPricing } from '@/features/pricing/api'
import { splitBillingExprAndRequestRules } from '@/features/pricing/lib/billing-expr'
import {
  parseFlatTokenPriceExpr,
  ratioToFlatTokenPrices,
} from '@/features/pricing/lib/flat-token-price'
import type { PricingModel } from '@/features/pricing/types'
import { useSystemOptions } from '../hooks/use-system-options'
import { useTaskPricingUsage } from '../hooks/use-task-pricing-usage'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeJsonParse } from '../utils/json-parser'
import { updateModelPricingOptions } from './model-pricing-options'
import { ModelRatioDialog, type ModelRatioData } from './model-ratio-dialog'
import {
  applyTaskRuleChange,
  modelTaskPricingFlags,
  parseTaskExpressions,
  pluginTaskPricing,
  taskPricingOverview,
  TASK_EXPRESSIONS_OPTION_KEY,
  type TaskPriceRule,
} from './task-pricing-sources'

type ModelRatioVisualEditorProps = {
  modelPrice: string
  modelRatio: string
  cacheRatio: string
  createCacheRatio: string
  completionRatio: string
  imageRatio: string
  audioRatio: string
  audioCompletionRatio: string
  batchInputRatio: string
  billingMode: string
  billingExpr: string
  candidateModelNames?: string[]
  filterMode?: 'all' | 'unset'
  /** Unsaved model price edits that an immediate plugin-price save would drop. */
  hasPendingChanges?: boolean
  /** Opens this model's dialog once per nonce. */
  openRequest?: { model: string; nonce: number }
  onOpenRequestHandled?: () => void
  onChange: (field: string, value: string) => void
}

type ModelRow = {
  name: string
  price?: string
  ratio?: string
  cacheRatio?: string
  createCacheRatio?: string
  completionRatio?: string
  imageRatio?: string
  audioRatio?: string
  audioCompletionRatio?: string
  knowledgeInput?: string
  knowledgeImage?: string
  knowledgeBatchInput?: string
  knowledge?: PricingModel['knowledge']
  billingMode?: string
  billingExpr?: string
  requestRuleExpr?: string
  storedBillingMode?: string
  flatPriceSummary?: string
  hasConflict: boolean
}

const STORAGE_KEY = 'model-price-column-visibility-v2'

const formatValue = (value?: string) => {
  if (!value || value === '') return '—'
  return value
}

function toEditData(model: ModelRow): ModelRatioData {
  return {
    name: model.name,
    price: model.price,
    ratio: model.ratio,
    cacheRatio: model.cacheRatio,
    createCacheRatio: model.createCacheRatio,
    completionRatio: model.completionRatio,
    imageRatio: model.imageRatio,
    audioRatio: model.audioRatio,
    audioCompletionRatio: model.audioCompletionRatio,
    knowledgeInput: model.knowledgeInput,
    knowledgeImage: model.knowledgeImage,
    knowledgeBatchInput: model.knowledgeBatchInput,
    knowledge: model.knowledge,
    billingMode:
      model.billingMode === 'tiered_expr' ||
      model.billingMode === 'flat_price' ||
      model.billingMode === 'audio_seconds'
        ? model.billingMode
        : model.price && model.price !== ''
          ? 'per-request'
          : 'per-token',
    billingExpr: model.billingExpr,
    requestRuleExpr: model.requestRuleExpr,
  }
}

export const ModelRatioVisualEditor = memo(
  function ModelRatioVisualEditor({
    modelPrice,
    modelRatio,
    cacheRatio,
    createCacheRatio,
    completionRatio,
    imageRatio,
    audioRatio,
    audioCompletionRatio,
    batchInputRatio,
    billingMode,
    billingExpr,
    candidateModelNames,
    filterMode = 'all',
    hasPendingChanges = false,
    openRequest,
    onOpenRequestHandled,
    onChange,
  }: ModelRatioVisualEditorProps) {
    const { t } = useTranslation()
    const pricingQuery = useQuery({
      queryKey: ['pricing'],
      queryFn: () => getPricing(),
    })
    const taskUsageQuery = useTaskPricingUsage()
    const { mutateAsync: updateOptionAsync, isPending: taskSaving } =
      useUpdateOption()
    const [dialogOpen, setDialogOpen] = useState(false)
    const [editData, setEditData] = useState<ModelRatioData | null>(null)
    const [sorting, setSorting] = useState<SortingState>([])
    const [pagination, setPagination] = useState<PaginationState>({
      pageIndex: 0,
      pageSize: 10,
    })
    const [columnVisibility, setColumnVisibility] = useState<VisibilityState>(
      () => {
        const saved = localStorage.getItem(STORAGE_KEY)
        if (saved) {
          try {
            return safeJsonParse<VisibilityState>(saved, {
              fallback: {
                cacheRatio: false,
                imageRatio: false,
                audioRatio: false,
                audioCompletionRatio: false,
              },
              silent: true,
            })
          } catch {
            return {
              cacheRatio: false,
              createCacheRatio: false,
              imageRatio: false,
              audioRatio: false,
              audioCompletionRatio: false,
            }
          }
        }
        return {
          cacheRatio: false,
          createCacheRatio: false,
          imageRatio: false,
          audioRatio: false,
          audioCompletionRatio: false,
        }
      }
    )

    useEffect(() => {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(columnVisibility))
    }, [columnVisibility])

    const { data: systemOptions } = useSystemOptions()
    const taskRulesRaw = systemOptions?.data?.find(
      (option) => option.key === TASK_EXPRESSIONS_OPTION_KEY
    )?.value
    const taskRules = useMemo(
      () => parseTaskExpressions(taskRulesRaw) ?? {},
      [taskRulesRaw]
    )
    const taskModels = useMemo(
      () =>
        new Map(
          (taskUsageQuery.data?.success ? taskUsageQuery.data.data : []).map(
            (model) => [model.model_name, model]
          )
        ),
      [taskUsageQuery.data]
    )
    const taskFlags = useMemo(
      () =>
        modelTaskPricingFlags(
          taskPricingOverview([...taskModels.values()], taskRules)
        ),
      [taskModels, taskRules]
    )
    // Same write as the former task price page: re-read the stored option,
    // change one plugin rule and write the whole map back.
    const saveTaskRule = useCallback(
      async (
        model: string,
        plugin: string,
        expression: string | TaskPriceRule | null
      ) => {
        const current = parseTaskExpressions(taskRulesRaw)
        if (!current) {
          toast.error(t('Stored plugin task prices are not valid JSON'))
          throw new Error('invalid task_expressions')
        }
        await updateOptionAsync({
          key: TASK_EXPRESSIONS_OPTION_KEY,
          value: JSON.stringify(
            applyTaskRuleChange(current, model, plugin, expression)
          ),
        })
      },
      [taskRulesRaw, updateOptionAsync, t]
    )
    const completionMeta =
      systemOptions?.data?.find(
        (option) => option.key === 'CompletionRatioMeta'
      )?.value || '{}'
    const models = useMemo(() => {
      const effectiveCompletions = JSON.parse(completionMeta) as Record<
        string,
        { ratio: number }
      >
      const priceMap = safeJsonParse<Record<string, number>>(modelPrice, {
        fallback: {},
        context: 'model prices',
      })
      const ratioMap = safeJsonParse<Record<string, number>>(modelRatio, {
        fallback: {},
        context: 'model ratios',
      })
      const cacheMap = safeJsonParse<Record<string, number>>(cacheRatio, {
        fallback: {},
        context: 'cache ratios',
      })
      const createCacheMap = safeJsonParse<Record<string, number>>(
        createCacheRatio,
        { fallback: {}, context: 'create cache ratios' }
      )
      const completionMap = safeJsonParse<Record<string, number>>(
        completionRatio,
        { fallback: {}, context: 'completion ratios' }
      )
      const imageMap = safeJsonParse<Record<string, number>>(imageRatio, {
        fallback: {},
        context: 'image ratios',
      })
      const audioMap = safeJsonParse<Record<string, number>>(audioRatio, {
        fallback: {},
        context: 'audio ratios',
      })
      const audioCompletionMap = safeJsonParse<Record<string, number>>(
        audioCompletionRatio,
        { fallback: {}, context: 'audio completion ratios' }
      )
      const billingModeMap = safeJsonParse<Record<string, string>>(
        billingMode,
        {
          fallback: {},
          context: 'billing mode',
        }
      )
      const billingExprMap = safeJsonParse<Record<string, string>>(
        billingExpr,
        {
          fallback: {},
          context: 'billing expression',
        }
      )
      const batchInputMap = safeJsonParse<Record<string, number>>(
        batchInputRatio,
        { fallback: {}, context: 'batch input ratios' }
      )

      const modelNames =
        filterMode === 'unset'
          ? new Set(candidateModelNames ?? [])
          : new Set([
              ...(pricingQuery.data?.data ?? [])
                .filter((model) => model.knowledge)
                .map((model) => model.model_name),
              ...Object.keys(priceMap),
              ...Object.keys(ratioMap),
              ...Object.keys(cacheMap),
              ...Object.keys(createCacheMap),
              ...Object.keys(completionMap),
              ...Object.keys(imageMap),
              ...Object.keys(audioMap),
              ...Object.keys(audioCompletionMap),
              ...Object.keys(billingModeMap),
              ...Object.keys(billingExprMap),
              ...Object.keys(batchInputMap),
            ])

      const knowledgeModels = new Map(
        (pricingQuery.data?.data ?? [])
          .filter((model) => model.knowledge)
          .map((model) => [model.model_name, model.knowledge])
      )

      const modelData: ModelRow[] = Array.from(modelNames).map((name) => {
        const price = priceMap[name]?.toString() || ''
        const ratio = ratioMap[name]?.toString() || ''
        const cache = cacheMap[name]?.toString() || ''
        const createCache = createCacheMap[name]?.toString() || ''
        const completion =
          (ratio !== ''
            ? effectiveCompletions[name]?.ratio
            : undefined
          )?.toString() ||
          completionMap[name]?.toString() ||
          ''
        const image = imageMap[name]?.toString() || ''
        const audio = audioMap[name]?.toString() || ''
        const audioCompletion = audioCompletionMap[name]?.toString() || ''
        const knowledge = knowledgeModels.get(name)
        const knowledgeInput =
          ratioMap[name] !== undefined ? ratioMap[name] * 2 : undefined
        const knowledgeImage =
          knowledgeInput !== undefined && imageMap[name] !== undefined
            ? knowledgeInput * imageMap[name]
            : undefined
        const knowledgeBatchInput =
          batchInputMap[name] !== undefined
            ? batchInputMap[name] * 2
            : undefined

        const modeForModel = billingModeMap[name]
        if (modeForModel === 'tiered_expr' || modeForModel === 'flat_price') {
          // Tiered_expr models may also retain ratio/price values as fallback
          // during multi-instance sync delays. We preserve them in the row so
          // the edit dialog round-trip and the next save don't drop them.
          const fullExpr = billingExprMap[name] || ''
          const { billingExpr: pureExpr, requestRuleExpr } =
            modeForModel === 'tiered_expr'
              ? splitBillingExprAndRequestRules(fullExpr)
              : { billingExpr: fullExpr, requestRuleExpr: '' }
          const flatPrices = parseFlatTokenPriceExpr(fullExpr)
          const effectiveMode = flatPrices ? 'flat_price' : modeForModel
          return {
            name,
            billingMode: effectiveMode,
            storedBillingMode: modeForModel,
            billingExpr: pureExpr,
            requestRuleExpr,
            flatPriceSummary: flatPrices
              ? `$${flatPrices.inputPrice} / $${flatPrices.outputPrice}`
              : '',
            price,
            ratio,
            cacheRatio: cache,
            createCacheRatio: createCache,
            completionRatio: completion,
            imageRatio: image,
            audioRatio: audio,
            audioCompletionRatio: audioCompletion,
            knowledgeInput: knowledgeInput?.toString() || '',
            knowledgeImage: knowledgeImage?.toString() || '',
            knowledgeBatchInput: knowledgeBatchInput?.toString() || '',
            knowledge,
            hasConflict: false,
          }
        }

        return {
          name,
          price,
          ratio,
          cacheRatio: cache,
          createCacheRatio: createCache,
          completionRatio: completion,
          imageRatio: image,
          audioRatio: audio,
          audioCompletionRatio: audioCompletion,
          knowledgeInput: knowledgeInput?.toString() || '',
          knowledgeImage: knowledgeImage?.toString() || '',
          knowledgeBatchInput: knowledgeBatchInput?.toString() || '',
          knowledge,
          billingMode:
            modeForModel === 'audio_seconds'
              ? 'audio_seconds'
              : price !== ''
                ? 'per-request'
                : 'per-token',
          hasConflict:
            price !== '' &&
            (ratio !== '' ||
              completion !== '' ||
              cache !== '' ||
              createCache !== '' ||
              image !== '' ||
              audio !== '' ||
              audioCompletion !== ''),
        }
      })

      return modelData
        .filter(
          (model) =>
            filterMode !== 'unset' ||
            (model.billingMode !== 'tiered_expr' &&
              model.billingMode !== 'flat_price' &&
              !model.price &&
              !model.ratio &&
              !Object.hasOwn(ratioMap, model.name))
        )
        .sort((a, b) => a.name.localeCompare(b.name))
    }, [
      modelPrice,
      modelRatio,
      cacheRatio,
      createCacheRatio,
      completionRatio,
      imageRatio,
      audioRatio,
      audioCompletionRatio,
      billingMode,
      billingExpr,
      batchInputRatio,
      pricingQuery.data,
      completionMeta,
      candidateModelNames,
      filterMode,
    ])

    const handleEdit = useCallback((model: ModelRow) => {
      setEditData(toEditData(model))
      setDialogOpen(true)
    }, [])

    // Requests from the task price overview: a model without a price yet
    // opens with only its name, so the admin can price it here.
    const [handledRequest, setHandledRequest] = useState<number>()
    if (openRequest && openRequest.nonce !== handledRequest) {
      setHandledRequest(openRequest.nonce)
      const row = models.find((model) => model.name === openRequest.model)
      setEditData(row ? toEditData(row) : { name: openRequest.model })
      setDialogOpen(true)
    }
    useEffect(() => {
      if (openRequest && openRequest.nonce === handledRequest)
        onOpenRequestHandled?.()
    }, [openRequest, handledRequest, onOpenRequestHandled])

    const handleAdd = useCallback(() => {
      setEditData(null)
      setDialogOpen(true)
    }, [])

    const handleDelete = useCallback(
      (name: string) => {
        const priceMap = safeJsonParse<Record<string, number>>(modelPrice, {
          fallback: {},
          silent: true,
        })
        const ratioMap = safeJsonParse<Record<string, number>>(modelRatio, {
          fallback: {},
          silent: true,
        })
        const cacheMap = safeJsonParse<Record<string, number>>(cacheRatio, {
          fallback: {},
          silent: true,
        })
        const createCacheMap = safeJsonParse<Record<string, number>>(
          createCacheRatio,
          { fallback: {}, silent: true }
        )
        const completionMap = safeJsonParse<Record<string, number>>(
          completionRatio,
          { fallback: {}, silent: true }
        )
        const imageMap = safeJsonParse<Record<string, number>>(imageRatio, {
          fallback: {},
          silent: true,
        })
        const audioMap = safeJsonParse<Record<string, number>>(audioRatio, {
          fallback: {},
          silent: true,
        })
        const audioCompletionMap = safeJsonParse<Record<string, number>>(
          audioCompletionRatio,
          { fallback: {}, silent: true }
        )
        const billingModeMap = safeJsonParse<Record<string, string>>(
          billingMode,
          { fallback: {}, silent: true }
        )
        const billingExprMap = safeJsonParse<Record<string, string>>(
          billingExpr,
          { fallback: {}, silent: true }
        )
        const batchInputMap = safeJsonParse<Record<string, number>>(
          batchInputRatio,
          { fallback: {}, silent: true }
        )

        delete priceMap[name]
        delete ratioMap[name]
        delete cacheMap[name]
        delete createCacheMap[name]
        delete completionMap[name]
        delete imageMap[name]
        delete audioMap[name]
        delete audioCompletionMap[name]
        delete billingModeMap[name]
        delete billingExprMap[name]
        delete batchInputMap[name]

        onChange('ModelPrice', JSON.stringify(priceMap, null, 2))
        onChange('ModelRatio', JSON.stringify(ratioMap, null, 2))
        onChange('CacheRatio', JSON.stringify(cacheMap, null, 2))
        onChange('CreateCacheRatio', JSON.stringify(createCacheMap, null, 2))
        onChange('CompletionRatio', JSON.stringify(completionMap, null, 2))
        onChange('ImageRatio', JSON.stringify(imageMap, null, 2))
        onChange('BatchInputRatio', JSON.stringify(batchInputMap, null, 2))
        onChange('AudioRatio', JSON.stringify(audioMap, null, 2))
        onChange(
          'AudioCompletionRatio',
          JSON.stringify(audioCompletionMap, null, 2)
        )
        onChange(
          'billing_setting.billing_mode',
          JSON.stringify(billingModeMap, null, 2)
        )
        onChange(
          'billing_setting.billing_expr',
          JSON.stringify(billingExprMap, null, 2)
        )
      },
      [
        modelPrice,
        modelRatio,
        cacheRatio,
        createCacheRatio,
        completionRatio,
        imageRatio,
        audioRatio,
        audioCompletionRatio,
        billingMode,
        billingExpr,
        batchInputRatio,
        onChange,
      ]
    )

    const columns = useMemo<ColumnDef<ModelRow>[]>(() => {
      return [
        {
          accessorKey: 'name',
          header: ({ column }) => (
            <DataTableColumnHeader column={column} title={t('Model name')} />
          ),
          cell: ({ row }) => (
            <div className='flex items-center gap-2 font-medium'>
              {row.getValue('name')}
              {row.original.billingMode === 'tiered_expr' && (
                <StatusBadge
                  label={t('Tiered')}
                  variant='info'
                  copyable={false}
                />
              )}
              {(row.original.billingMode === 'flat_price' ||
                row.original.billingMode === 'per-token') && (
                <StatusBadge
                  label={t('Fixed token price')}
                  variant='success'
                  copyable={false}
                />
              )}
              {row.original.knowledge && (
                <StatusBadge
                  label={t(
                    row.original.knowledge.kind === 'rerank'
                      ? 'Rerank'
                      : 'Embedding'
                  )}
                  variant='info'
                  copyable={false}
                />
              )}
              {row.original.hasConflict && (
                <StatusBadge
                  label={t('Conflict')}
                  variant='danger'
                  copyable={false}
                />
              )}
              {taskFlags[row.original.name]?.override && (
                <StatusBadge
                  label={t('Plugin-specific pricing')}
                  variant='info'
                  copyable={false}
                />
              )}
              {taskFlags[row.original.name]?.needsPrice && (
                <StatusBadge
                  label={t('Plugin task needs a price')}
                  variant='danger'
                  copyable={false}
                />
              )}
            </div>
          ),
          enableHiding: false,
        },
        {
          accessorKey: 'price',
          header: ({ column }) => (
            <DataTableColumnHeader column={column} title={t('Fixed price')} />
          ),
          cell: ({ row }) => (
            <span
              className={
                row.original.billingMode === 'tiered_expr'
                  ? 'text-muted-foreground'
                  : ''
              }
            >
              {row.original.billingMode === 'flat_price'
                ? formatValue(row.original.flatPriceSummary)
                : formatValue(row.getValue('price'))}
            </span>
          ),
          meta: { label: 'Fixed price' },
        },
        ...(
          [
            ['inputPrice', 'Input price'],
            ['outputPrice', 'Output price'],
            ['cacheReadPrice', 'Cache read price'],
            ['cacheCreatePrice', 'Cache write price (5m)'],
            ['cacheCreate1hPrice', 'Cache write price (1h)'],
            ['imageInputPrice', 'Image input price'],
            ['imageOutputPrice', 'Image output price'],
            ['audioInputPrice', 'Audio input price'],
            ['audioOutputPrice', 'Audio output price'],
            ['batchInputPrice', 'Batch input price'],
          ] as const
        ).map(
          ([field, label]): ColumnDef<ModelRow> => ({
            id: field,
            header: ({ column }) => (
              <DataTableColumnHeader column={column} title={t(label)} />
            ),
            cell: ({ row }) => {
              const model = row.original
              if (
                model.billingMode === 'per-request' ||
                model.billingMode === 'tiered_expr'
              )
                return '—'
              const prices =
                model.billingMode === 'flat_price'
                  ? parseFlatTokenPriceExpr(model.billingExpr || '')
                  : ratioToFlatTokenPrices(model)
              return formatValue(
                field === 'batchInputPrice'
                  ? model.knowledgeBatchInput
                  : prices?.[field]
              )
            },
            meta: { label },
          })
        ),
        {
          id: 'actions',
          cell: ({ row }) => (
            <div className='flex justify-end gap-2'>
              <Button
                variant='ghost'
                size='sm'
                onClick={() => handleEdit(row.original)}
              >
                <Pencil className='h-4 w-4' />
              </Button>
              <Button
                variant='ghost'
                size='sm'
                onClick={() => handleDelete(row.original.name)}
                disabled={filterMode === 'unset'}
              >
                <Trash2 className='h-4 w-4' />
              </Button>
            </div>
          ),
          enableHiding: false,
        },
      ]
    }, [handleEdit, handleDelete, filterMode, taskFlags, t])

    const table = useReactTable({
      data: models,
      columns,
      state: {
        sorting,
        columnVisibility,
        pagination,
      },
      onSortingChange: setSorting,
      onColumnVisibilityChange: setColumnVisibility,
      onPaginationChange: setPagination,
      autoResetPageIndex: false,
      getCoreRowModel: getCoreRowModel(),
      getFilteredRowModel: getFilteredRowModel(),
      getSortedRowModel: getSortedRowModel(),
      getPaginationRowModel: getPaginationRowModel(),
      globalFilterFn: (row, _columnId, filterValue) => {
        const searchValue = String(filterValue).toLowerCase()
        return row.original.name.toLowerCase().includes(searchValue)
      },
    })

    const handleSave = useCallback(
      (data: ModelRatioData) => {
        const updates = updateModelPricingOptions(
          {
            ModelPrice: modelPrice,
            ModelRatio: modelRatio,
            CompletionRatio: completionRatio,
            CacheRatio: cacheRatio,
            CreateCacheRatio: createCacheRatio,
            ImageRatio: imageRatio,
            AudioRatio: audioRatio,
            AudioCompletionRatio: audioCompletionRatio,
            BatchInputRatio: batchInputRatio,
            'billing_setting.billing_mode': billingMode,
            'billing_setting.billing_expr': billingExpr,
          },
          data.name,
          data
        )
        for (const update of updates) onChange(update.key, update.value)
      },
      [
        modelPrice,
        modelRatio,
        completionRatio,
        cacheRatio,
        createCacheRatio,
        imageRatio,
        audioRatio,
        audioCompletionRatio,
        batchInputRatio,
        billingMode,
        billingExpr,
        onChange,
      ]
    )

    return (
      <div className='space-y-4'>
        <div className='flex items-center justify-between gap-4'>
          <DataTableToolbar
            table={table}
            searchPlaceholder={t('Search models...')}
          />
          {filterMode !== 'unset' && (
            <Button onClick={handleAdd}>
              <Plus className='mr-2 h-4 w-4' />
              {t('Add model')}
            </Button>
          )}
        </div>

        {table.getRowModel().rows.length === 0 ? (
          <div className='text-muted-foreground rounded-lg border border-dashed p-8 text-center'>
            {table.getState().globalFilter
              ? t('No models match your search')
              : filterMode === 'unset'
                ? t('No models with unset prices')
                : t('No models configured. Click "Add model" to get started.')}
          </div>
        ) : (
          <div className='overflow-hidden rounded-md border'>
            <Table>
              <TableHeader>
                {table.getHeaderGroups().map((headerGroup) => (
                  <TableRow key={headerGroup.id}>
                    {headerGroup.headers.map((header) => (
                      <TableHead key={header.id} colSpan={header.colSpan}>
                        {header.isPlaceholder
                          ? null
                          : flexRender(
                              header.column.columnDef.header,
                              header.getContext()
                            )}
                      </TableHead>
                    ))}
                  </TableRow>
                ))}
              </TableHeader>
              <TableBody>
                {table.getRowModel().rows.map((row) => (
                  <TableRow key={row.id}>
                    {row.getVisibleCells().map((cell) => (
                      <TableCell key={cell.id}>
                        {flexRender(
                          cell.column.columnDef.cell,
                          cell.getContext()
                        )}
                      </TableCell>
                    ))}
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        {table.getRowModel().rows.length > 0 && (
          <DataTablePagination table={table} />
        )}

        <ModelRatioDialog
          billingUsageByModel={Object.fromEntries(
            (pricingQuery.data?.data ?? []).map((model) => [
              model.model_name,
              model.billing_usage,
            ])
          )}
          taskPricing={
            editData
              ? {
                  plugins: pluginTaskPricing(
                    taskModels.get(editData.name),
                    editData.name,
                    taskRules
                  ),
                  saving: taskSaving,
                  lockedReason: hasPendingChanges
                    ? t(
                        'Save the pending model price changes first: saving a plugin price reloads the settings and would discard them.'
                      )
                    : undefined,
                  onSave: (plugin, expression) =>
                    saveTaskRule(editData.name, plugin, expression),
                }
              : undefined
          }
          open={dialogOpen}
          onOpenChange={setDialogOpen}
          onSave={handleSave}
          editData={editData}
        />
      </div>
    )
  },
  // Custom equality check - only re-render if JSON props actually changed
  (prevProps, nextProps) => {
    return (
      prevProps.modelPrice === nextProps.modelPrice &&
      prevProps.modelRatio === nextProps.modelRatio &&
      prevProps.cacheRatio === nextProps.cacheRatio &&
      prevProps.createCacheRatio === nextProps.createCacheRatio &&
      prevProps.completionRatio === nextProps.completionRatio &&
      prevProps.imageRatio === nextProps.imageRatio &&
      prevProps.audioRatio === nextProps.audioRatio &&
      prevProps.audioCompletionRatio === nextProps.audioCompletionRatio &&
      prevProps.billingMode === nextProps.billingMode &&
      prevProps.billingExpr === nextProps.billingExpr &&
      prevProps.candidateModelNames === nextProps.candidateModelNames &&
      prevProps.filterMode === nextProps.filterMode &&
      prevProps.hasPendingChanges === nextProps.hasPendingChanges &&
      prevProps.openRequest === nextProps.openRequest &&
      prevProps.onOpenRequestHandled === nextProps.onOpenRequestHandled &&
      prevProps.onChange === nextProps.onChange
    )
  }
)
