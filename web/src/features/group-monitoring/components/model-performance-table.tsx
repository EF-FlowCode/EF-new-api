import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { Empty, EmptyDescription } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  formatMetricTime,
  formatMetricTps,
  modelTimelineSlots,
  performanceColor,
} from '../lib/model-performance'
import type { MonitoringModel, MonitoringModelsResult } from '../types'

export type ModelPerformanceTableProps = {
  models?: MonitoringModel[]
  window?: MonitoringModelsResult
  loading: boolean
  error: boolean
}

export function ModelPerformanceTable({
  models,
  window,
  loading,
  error,
}: ModelPerformanceTableProps) {
  const { t } = useTranslation()
  // 按模型数量均分，先读左栏再读右栏；手机通过 CSS 按相同顺序堆叠。
  const splitAt = Math.ceil((models?.length ?? 0) / 2)
  const columns =
    models && models.length > 4
      ? [models.slice(0, splitAt), models.slice(splitAt)]
      : [models ?? []]
  return (
    <div className='flex min-w-0 flex-col gap-2'>
      <div className='text-muted-foreground flex flex-wrap items-center justify-between gap-2 text-xs'>
        <span>{t('Model performance · Last 24 hours')}</span>
        {error && window && (
          <span role='status'>
            {t('Refresh failed; showing previous model data')}
          </span>
        )}
      </div>
      {loading ? (
        <div className='flex flex-col gap-3 py-3'>
          {[0, 1, 2].map((row) => (
            <Skeleton key={row} className='h-5 w-full' />
          ))}
        </div>
      ) : !models?.length || !window ? (
        <Empty className='p-4 md:p-4'>
          <EmptyDescription>
            {error
              ? t('Failed to load model monitoring data')
              : t('No model samples in the last 24 hours')}
          </EmptyDescription>
        </Empty>
      ) : (
        <TooltipProvider delayDuration={150}>
          <div
            className={cn(
              'grid min-w-0 grid-cols-1 gap-3 md:gap-6',
              columns.length === 2 && 'md:grid-cols-2'
            )}
          >
            {columns.map((column, index) => (
              <div key={index} className='min-w-0'>
                <Table className='min-w-[620px]'>
                  <TableHeader className='[&_th]:h-8'>
                    <TableRow>
                      <TableHead>{t('Model')}</TableHead>
                      <TableHead className='text-right'>
                        {t('First token')}
                      </TableHead>
                      <TableHead className='text-right'>
                        {t('Latency')}
                      </TableHead>
                      <TableHead className='text-right'>
                        {t('Throughput')}
                      </TableHead>
                      <TableHead className='text-right'>
                        {t('Success rate')}
                      </TableHead>
                      <TableHead className='w-full min-w-[180px]'>
                        {t('Trend')}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {column.map((model) => (
                      <TableRow key={model.model_name} className='border-0'>
                        <TableCell className='max-w-[240px] py-1'>
                          <span
                            className='block truncate'
                            title={model.model_name}
                          >
                            {model.model_name}
                          </span>
                        </TableCell>
                        <TableCell className='py-1 text-right font-mono text-xs'>
                          {formatMetricTime(model.avg_ttft_ms)}
                        </TableCell>
                        <TableCell className='py-1 text-right font-mono text-xs'>
                          {formatMetricTime(model.avg_latency_ms)}
                        </TableCell>
                        <TableCell className='py-1 text-right font-mono text-xs'>
                          {formatMetricTps(model.avg_tps)}
                        </TableCell>
                        <TableCell
                          className='py-1 text-right font-mono text-xs'
                          style={{
                            color: performanceColor(model.success_rate),
                          }}
                        >
                          {model.success_rate.toFixed(2)}%
                        </TableCell>
                        <TableCell className='py-1'>
                          <div
                            className='flex w-full gap-px'
                            aria-label={`${model.model_name} · ${t('Success rate')}`}
                          >
                            {modelTimelineSlots(model.series, window).map(
                              ({ ts, end, point }) => {
                                const time = `${new Date(ts * 1000).toLocaleString([], { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })} – ${new Date(end * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })}`
                                const rate = point
                                  ? `${point.success_rate.toFixed(2)}%`
                                  : t('No data')
                                return (
                                  <Tooltip key={ts}>
                                    <TooltipTrigger asChild>
                                      <button
                                        type='button'
                                        className='focus-visible:outline-ring h-3 min-w-0 flex-1 focus-visible:outline-2 focus-visible:outline-offset-2'
                                        style={{
                                          background: performanceColor(
                                            point?.success_rate
                                          ),
                                        }}
                                        aria-label={`${time} · ${t('Success rate')}: ${rate}`}
                                      />
                                    </TooltipTrigger>
                                    <TooltipContent>
                                      <div className='flex flex-col gap-1 text-xs'>
                                        <span>{time}</span>
                                        <span>
                                          {t('Success rate')}: {rate}
                                        </span>
                                        {point && (
                                          <>
                                            <span>
                                              {t('First token')}:{' '}
                                              {formatMetricTime(
                                                point.avg_ttft_ms
                                              )}
                                            </span>
                                            <span>
                                              {t('Latency')}:{' '}
                                              {formatMetricTime(
                                                point.avg_latency_ms
                                              )}
                                            </span>
                                            <span>
                                              {t('Throughput')}:{' '}
                                              {formatMetricTps(point.avg_tps)}
                                            </span>
                                          </>
                                        )}
                                      </div>
                                    </TooltipContent>
                                  </Tooltip>
                                )
                              }
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ))}
          </div>
        </TooltipProvider>
      )}
    </div>
  )
}
