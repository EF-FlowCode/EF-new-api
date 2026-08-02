import { createFileRoute } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useIsAdmin } from '@/hooks/use-admin'
import { useStatus } from '@/hooks/use-status'
import { Skeleton } from '@/components/ui/skeleton'
import { FeatureUnavailable } from '@/components/feature-unavailable'
import { AppHeader, Main } from '@/components/layout'
import { GroupMonitoring } from '@/features/group-monitoring'

export const Route = createFileRoute('/_authenticated/group-monitoring/')({
  component: GroupMonitoringPage,
})

function GroupMonitoringPage() {
  const { t } = useTranslation()
  const { status, loading } = useStatus()
  const isAdmin = useIsAdmin()

  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        {loading && !status ? (
          <Skeleton className='mx-auto my-10 h-48 w-[min(90%,56rem)]' />
        ) : status?.group_monitoring_available === true || isAdmin ? (
          <GroupMonitoring />
        ) : (
          <FeatureUnavailable
            title={t('Group monitoring is not available')}
            description={t(
              'No public monitoring groups are available right now.'
            )}
          />
        )}
      </Main>
    </>
  )
}
