import { Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { REQUEST_ENDPOINT_TYPE_OPTIONS } from '../../constants'
import type { EndpointSupport } from '../../lib/endpoint-support'

const CHANNEL_TYPE_CODEX = 57

type EndpointSupportSummaryProps = {
  support: EndpointSupport
  /** The manual override is set and replaces the automatic list. */
  overridden: boolean
  currentType: number
  onEditRoutes: () => void
}

function endpointLabel(value: string): string {
  return (
    REQUEST_ENDPOINT_TYPE_OPTIONS.find((option) => option.value === value)
      ?.label ?? value
  )
}

export function EndpointSupportSummary({
  support,
  overridden,
  currentType,
  onEditRoutes,
}: EndpointSupportSummaryProps) {
  const { t } = useTranslation()

  const description = (() => {
    switch (support.source) {
      case 'preset':
        return t('Fixed by the built-in route preset of this channel type.')
      case 'routes':
        return t(
          'Determined by the Advanced Custom routes. Requests for other endpoints are not routed to this channel.'
        )
      case 'legacy-custom':
        return t(
          'Custom channels without relay routes accept every endpoint. Add Advanced Custom routes to restrict them.'
        )
      default:
        return overridden
          ? t(
              'Automatic list for this channel type; replaced by the manual override below.'
            )
          : t(
              'Derived automatically from the channel type. Requests for other endpoints are routed to other channels.'
            )
    }
  })()

  return (
    <div className='bg-muted/40 space-y-2 rounded-lg border p-3'>
      <div className='flex flex-wrap items-center gap-1.5'>
        {support.endpoints === null ? (
          <Badge variant='secondary'>{t('All endpoints')}</Badge>
        ) : support.endpoints.length === 0 ? (
          <Badge variant='outline'>{t('No relay endpoints')}</Badge>
        ) : (
          support.endpoints.map((endpoint) => (
            <Badge
              key={endpoint}
              variant={overridden ? 'outline' : 'secondary'}
              className={overridden ? 'line-through opacity-70' : undefined}
            >
              {t(endpointLabel(endpoint))}
            </Badge>
          ))
        )}
        {(support.source === 'routes' ||
          support.source === 'legacy-custom') && (
          <Button
            type='button'
            variant='ghost'
            size='sm'
            className='ml-auto h-7'
            onClick={onEditRoutes}
          >
            <Route className='mr-1.5 size-3.5' aria-hidden='true' />
            {t('Edit routes')}
          </Button>
        )}
      </div>
      <p className='text-muted-foreground text-xs'>{description}</p>
      {support.source === 'type' && currentType === CHANNEL_TYPE_CODEX && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Chat Completions and Anthropic Messages are also accepted when the global Chat→Responses policy covers this channel.'
          )}
        </p>
      )}
    </div>
  )
}
