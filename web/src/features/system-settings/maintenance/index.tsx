import { useMemo } from 'react'
import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { useStatus } from '@/hooks/use-status'
import { getDomainTabItems } from '@/components/layout/config/admin-domains'
import { SettingsWorkbench } from '../components/settings-workbench'
import { getOptionValue, useSystemOptions } from '../hooks/use-system-options'
import { DEFAULT_MAINTENANCE_SETTINGS } from './config'
import {
  MAINTENANCE_DEFAULT_SECTION,
  getMaintenanceSectionContent,
  type MaintenanceSectionId,
} from './section-registry.tsx'

export function MaintenanceSettings() {
  const { t } = useTranslation()
  const { data, isLoading } = useSystemOptions()
  const { status } = useStatus()
  const params = useParams({
    from: '/_authenticated/system-settings/maintenance/$section',
  })

  const settings = useMemo(
    () => getOptionValue(data?.data, DEFAULT_MAINTENANCE_SETTINGS),
    [data?.data]
  )

  if (isLoading) {
    return (
      <div className='text-muted-foreground flex h-full w-full flex-1 items-center justify-center'>
        {t('Loading maintenance settings...')}
      </div>
    )
  }

  const activeSection = (params?.section ??
    MAINTENANCE_DEFAULT_SECTION) as MaintenanceSectionId
  const sectionContent = getMaintenanceSectionContent(
    activeSection,
    settings,
    status?.version as string | undefined,
    status?.start_time as number | null | undefined
  )

  return (
    <div className='flex h-full w-full flex-1 flex-col'>
      <SettingsWorkbench
        items={getDomainTabItems(t, 'maintenance', activeSection)}
        activeSection={`maintenance/${activeSection}`}
      >
        {sectionContent}
      </SettingsWorkbench>
    </div>
  )
}
