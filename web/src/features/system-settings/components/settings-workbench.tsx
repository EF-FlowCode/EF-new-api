import type { ReactNode } from 'react'
import {
  SettingsSectionTabs,
  type SettingsSectionNavItem,
} from './settings-section-tabs'

type SettingsWorkbenchProps = {
  items: SettingsSectionNavItem[]
  activeSection: string
  children: ReactNode
}

/**
 * Shared high-density settings workbench.
 * The section navigation stays visible while the active form owns the right pane.
 */
export function SettingsWorkbench({
  items,
  activeSection,
  children,
}: SettingsWorkbenchProps) {
  return (
    <div className='faded-bottom h-full w-full overflow-y-auto scroll-smooth pb-12'>
      <div className='mx-auto w-full max-w-[1600px] min-w-0'>
        <SettingsSectionTabs items={items} activeSection={activeSection} />
        <div className='min-w-0 space-y-4'>{children}</div>
      </div>
    </div>
  )
}
