import { AlertTriangle, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { IconWan } from '@/assets/custom/icon-wan'
import { getLobeIcon } from '@/lib/lobe-icon'
import { resolveModelProvider } from '@/lib/model-provider'
import { cn } from '@/lib/utils'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import { StatusBadge } from '@/components/status-badge'
import {
  hasResponseModelDetails,
  isResponseModelMismatch,
  type ResponseModelObservation,
} from '../lib/response-model'
import type { LogOtherData } from '../types'

interface ModelBadgeProps {
  modelName: string
  actualModel?: string
  responseModel?: LogOtherData['response_model']
  className?: string
}

// eflow: @lobehub/icons 没有的供应商图标在这里补（按上游 d92612038 的 Wan 图标）。
// 不改 @/lib/lobe-icon，免得与图标按需加载的改造冲突。
const CUSTOM_PROVIDER_ICONS: Record<
  string,
  (props: { size?: number }) => React.ReactNode
> = {
  Wan: IconWan,
}

function renderProviderIcon(icon: string, size: number): React.ReactNode {
  const Custom = CUSTOM_PROVIDER_ICONS[icon]
  return Custom ? <Custom size={size} /> : getLobeIcon(icon, size)
}

function ModelBadgeContent(props: ModelBadgeProps) {
  const provider = resolveModelProvider(props.modelName)

  return (
    <StatusBadge
      copyText={props.modelName}
      size='sm'
      showDot={!provider}
      autoColor={provider ? undefined : props.modelName}
      className={cn(
        'border-border/60 bg-muted/30 rounded-md border px-1.5 py-0.5 font-mono',
        provider && 'text-foreground',
        props.className
      )}
    >
      <span className='flex items-center gap-1.5'>
        {provider && (
          <span
            className='flex size-3.5 shrink-0 items-center justify-center'
            title={provider.label ?? provider.name}
            aria-label={provider.label ?? provider.name}
          >
            {renderProviderIcon(provider.icon, 14)}
          </span>
        )}
        <span>{props.modelName}</span>
      </span>
    </StatusBadge>
  )
}

export function ModelBadge(props: ModelBadgeProps) {
  const { t } = useTranslation()
  // eflow: 按上游 ed7c4e35d + 972aed197，响应模型与请求不一致时在徽章旁警示；
  // 不一致按名称现算（isResponseModelMismatch），不读存储的标志位。
  const mismatch = isResponseModelMismatch(props.responseModel)
  const responseModelLabel =
    mismatch && props.responseModel
      ? t('Response model: {{model}}', {
          model: props.responseModel.returned_model,
        })
      : ''

  if (!props.actualModel && !hasResponseModelDetails(props.responseModel)) {
    return <ModelBadgeContent {...props} />
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type='button'
          className='inline-flex flex-wrap items-center gap-1'
          aria-label={`${t('Model')}: ${props.modelName}${responseModelLabel ? `, ${responseModelLabel}` : ''}`}
        >
          <ModelBadgeContent {...props} />
          {mismatch ? (
            <StatusBadge
              icon={AlertTriangle}
              label={responseModelLabel}
              variant='warning'
              showDot={false}
              copyable={false}
            />
          ) : (
            <Route className='text-muted-foreground size-3 shrink-0' />
          )}
        </button>
      </PopoverTrigger>
      <PopoverContent className='w-96 max-w-[calc(100vw-2rem)]'>
        {props.responseModel ? (
          <ResponseModelDetails observation={props.responseModel} />
        ) : (
          <div className='space-y-2'>
            <ModelRow label={t('Request Model')} value={props.modelName} />
            <ModelRow label={t('Actual Model')} value={props.actualModel} />
          </div>
        )}
      </PopoverContent>
    </Popover>
  )
}

function ModelRow(props: { label: string; value?: string }) {
  return (
    <div className='flex items-start justify-between gap-3'>
      <span className='text-muted-foreground shrink-0 text-xs'>
        {props.label}
      </span>
      <span className='min-w-0 font-mono text-xs font-medium break-all'>
        {props.value || '-'}
      </span>
    </div>
  )
}

/** 请求模型 / 上游请求模型 / 响应模型三行对照，不一致时带警示与说明。 */
export function ResponseModelDetails(props: {
  observation: ResponseModelObservation
}) {
  const { t } = useTranslation()
  const mismatch = isResponseModelMismatch(props.observation)

  return (
    <div className='min-w-0 space-y-2'>
      {mismatch && (
        <StatusBadge
          icon={AlertTriangle}
          label={t('Response model: {{model}}', {
            model: props.observation.returned_model,
          })}
          variant='warning'
          showDot={false}
          copyable={false}
          className='h-auto whitespace-normal'
        />
      )}
      <ModelRow
        label={t('Request Model')}
        value={props.observation.requested_model}
      />
      <ModelRow
        label={t('Upstream Model')}
        value={
          props.observation.upstream_model || props.observation.requested_model
        }
      />
      <ModelRow
        label={t('Response Model')}
        value={props.observation.returned_model}
      />
      {mismatch && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'The upstream returned a model name different from both the requested and upstream models. Aliases or dated versions may also cause this; this warning alone does not prove model substitution.'
          )}
        </p>
      )}
    </div>
  )
}
