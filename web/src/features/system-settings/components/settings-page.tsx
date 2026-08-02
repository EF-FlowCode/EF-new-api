import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { getDomainTabItems } from '@/components/layout/config/admin-domains'
import { useSystemOptions, getOptionValue } from '../hooks/use-system-options'
import { SettingsSectionTabs } from './settings-section-tabs'

type SettingsPageProps<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
> = {
  routePath: string
  defaultSettings: TSettings
  defaultSection: TSectionId
  getSectionContent: (
    sectionId: TSectionId,
    settings: TSettings,
    ...extraArgs: unknown[]
  ) => React.ReactNode
  // eflow: 传入设置区名（general/models/auth/...）则在内容上方渲染跨区「领域 tab」。
  area?: string
  extraArgs?: unknown[]
}

/**
 * Generic settings page component
 * Handles loading state, data fetching, and section rendering
 */
export function SettingsPage<
  TSettings extends Record<string, string | number | boolean | unknown[]>,
  TSectionId extends string,
>({
  routePath,
  defaultSettings,
  defaultSection,
  getSectionContent,
  area,
  extraArgs = [],
}: SettingsPageProps<TSettings, TSectionId>) {
  const { t } = useTranslation()
  const { data, isLoading } = useSystemOptions()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const params = useParams({ from: routePath as any })

  if (isLoading) {
    return (
      <div className='flex items-center justify-center py-12'>
        <div className='text-muted-foreground'>{t('Loading settings...')}</div>
      </div>
    )
  }

  const settings = getOptionValue(data?.data, defaultSettings) as TSettings
  const activeSection = (params?.section ?? defaultSection) as TSectionId
  const sectionContent = getSectionContent(
    activeSection,
    settings,
    ...extraArgs
  )
  const navItems = area
    ? getDomainTabItems(t, area, String(activeSection))
    : []

  return (
    <div className='flex h-full w-full flex-1 flex-col'>
      <div className='faded-bottom h-full w-full overflow-y-auto scroll-smooth pe-4 pb-12'>
        <div className='space-y-4'>
          <SettingsSectionTabs
            items={navItems}
            activeSection={
              area ? `${area}/${activeSection}` : String(activeSection)
            }
          />
          {sectionContent}
        </div>
      </div>
    </div>
  )
}
