import { useState } from 'react'
import { Pencil } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useSystemConfigStore } from '@/stores/system-config-store'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { useSystemOptions } from '../hooks/use-system-options'
import { useTaskPricingUsage } from '../hooks/use-task-pricing-usage'
import { TaskPricingStatusBadge } from './task-pricing-editor'
import { TaskPricingMigrationPanel } from './task-pricing-migration-panel'
import {
  borrowedPriceModel,
  classifyTaskSource,
  mappedModels,
  parseTaskExpressions,
  summarizeTaskRule,
  taskPricingOverview,
  TASK_EXPRESSIONS_OPTION_KEY,
  type TaskPricingOverviewRow,
} from './task-pricing-sources'

/** Read-only: prices are edited in the model price dialog. */
export function TaskPricingSettings({
  onEditModel,
}: {
  onEditModel: (model: string) => void
}) {
  const options = useSystemOptions()
  const pricing = useTaskPricingUsage()
  const { t } = useTranslation()
  if (options.isPending || pricing.isPending) return <p>{t('Loading...')}</p>
  if (!options.data?.success || !pricing.data?.success)
    return <p>{t('Failed to load settings')}</p>
  const rules = parseTaskExpressions(
    options.data.data.find((item) => item.key === TASK_EXPRESSIONS_OPTION_KEY)
      ?.value
  )
  return (
    <div className='flex flex-col gap-4'>
      <TaskPricingMigrationPanel />
      {rules === null && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Stored plugin task prices are not valid JSON')}
          </AlertDescription>
        </Alert>
      )}
      <TaskPricingOverview
        rows={taskPricingOverview(pricing.data.data, rules ?? {})}
        onEditModel={onEditModel}
      />
    </div>
  )
}

export function TaskPricingOverview({
  rows,
  onEditModel,
}: {
  rows: TaskPricingOverviewRow[]
  onEditModel: (model: string) => void
}) {
  const { t } = useTranslation()
  const [search, setSearch] = useState('')
  const query = search.trim().toLowerCase()
  const visible = query
    ? rows.filter(
        (row) =>
          row.model.toLowerCase().includes(query) ||
          row.plugin.toLowerCase().includes(query)
      )
    : rows
  const needsPrice = rows.filter((row) => row.status === 'needs_price').length
  return (
    <div className='flex flex-col gap-3'>
      <Alert>
        <AlertDescription>
          {t(
            'Plugin tasks follow the model price by default. To change a task price, edit the model: plugin-specific prices are set in the model price dialog.'
          )}
        </AlertDescription>
      </Alert>
      {needsPrice > 0 && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Plugin tasks that need a price: {{count}}', {
              count: needsPrice,
            })}
          </AlertDescription>
        </Alert>
      )}
      <Input
        value={search}
        onChange={(event) => setSearch(event.target.value)}
        placeholder={t('Search models...')}
        className='max-w-xs'
      />
      {visible.length === 0 ? (
        <div className='text-muted-foreground rounded-lg border border-dashed p-8 text-center'>
          {t('No plugin task prices')}
        </div>
      ) : (
        <div className='overflow-hidden rounded-md border'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('Plugin')}</TableHead>
                <TableHead>{t('Model name')}</TableHead>
                <TableHead>{t('Price source')}</TableHead>
                <TableHead>{t('Effective task price')}</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((row) => (
                <TableRow
                  key={`${row.plugin}/${row.model}`}
                  data-testid={`task-row-${row.plugin}-${row.model}`}
                  className={cn(
                    row.status === 'needs_price' && 'bg-destructive/5'
                  )}
                >
                  <TableCell className='font-medium'>{row.plugin}</TableCell>
                  <TableCell>
                    <div>{row.model}</div>
                    {mappedModels(row, row.model).length > 0 && (
                      <div className='text-muted-foreground text-xs'>
                        → {mappedModels(row, row.model).join(', ')}
                      </div>
                    )}
                  </TableCell>
                  <TableCell>
                    <TaskPricingStatusBadge status={row.status} />
                  </TableCell>
                  <TableCell className='max-w-md'>
                    <TaskPriceSummary row={row} />
                  </TableCell>
                  <TableCell className='text-right'>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      aria-label={`${t('Edit')} ${row.model}`}
                      onClick={() => onEditModel(row.model)}
                    >
                      <Pencil className='mr-1 h-4 w-4' />
                      {t('Edit')}
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

function TaskPriceSummary({ row }: { row: TaskPricingOverviewRow }) {
  const { t } = useTranslation()
  const quotaPerUnit = useSystemConfigStore(
    (state) => state.config.currency.quotaPerUnit
  )
  if (row.status === 'needs_price')
    return (
      <span className='text-destructive font-medium'>
        {t('Price required')}
      </span>
    )
  if (row.status === 'unavailable')
    return <span className='text-muted-foreground'>—</span>
  const effective =
    row.rules.find((rule) => classifyTaskSource(rule.source) === row.status) ??
    row.rules[0]
  const summary = summarizeTaskRule(
    row.status === 'override' ? row.override : effective?.rule
  )
  const expression =
    summary.kind === 'expression'
      ? summary.expression
      : row.status !== 'override'
        ? effective?.expression
        : undefined
  const borrowed = borrowedPriceModel(row, row.model)
  return (
    <div className='flex flex-col gap-1'>
      {row.status === 'follow' && (
        <span className='text-muted-foreground'>
          {t('Follows model price')}
        </span>
      )}
      {borrowed && (
        <span className='text-muted-foreground'>
          {t('Price of {{model}}', { model: borrowed })}
        </span>
      )}
      {summary.kind === 'unit_price' && (
        <span>
          {t('Task unit price (USD)')}: {summary.unitPrice}
        </span>
      )}
      {summary.kind === 'token_completion' && (
        <span>
          {t('USD per million completion tokens')}:{' '}
          {(summary.modelRatio * 1000000) / quotaPerUnit}
        </span>
      )}
      {expression && (
        <code className='text-muted-foreground line-clamp-2 text-xs break-all'>
          {expression}
        </code>
      )}
    </div>
  )
}
