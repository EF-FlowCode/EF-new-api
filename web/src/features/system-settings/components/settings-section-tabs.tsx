import { useNavigate } from '@tanstack/react-router'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'

// eflow: 上游每个系统设置大类（content / maintenance / integrations / ...）下面有多个
// section，原本靠这条"页内二级 tab"在它们之间切换。eflow 把左侧栏收成「每类一个入口」
// 后，这条 tab 在各设置页里没被渲染出来，导致除默认 section 外的子分区（公告 / FAQ /
// Notice / 日志 / worker / ionet ...）在 UI 里彻底没入口、只能手敲 URL。这里把这条
// 导航补回来：范式照搬 features/dashboard 的 Tabs + onValueChange→navigate。

export type SettingsSectionNavItem = {
  id: string
  title: string
  url: string
}

type SettingsSectionTabsProps = {
  items: SettingsSectionNavItem[]
  activeSection: string
}

export function SettingsSectionTabs({
  items,
  activeSection,
}: SettingsSectionTabsProps) {
  const navigate = useNavigate()

  // 单一 section 不需要 tab
  if (items.length <= 1) {
    return null
  }

  const handleChange = (id: string) => {
    const target = items.find((item) => item.id === id)
    if (target) {
      // url 已是 path 形式（/system-settings/<area>/<section>），直接整串导航
      void navigate({ to: target.url })
    }
  }

  return (
    <Tabs
      value={activeSection}
      onValueChange={handleChange}
      className='mb-2 overflow-x-auto pb-1'
    >
      <TabsList className='bg-card h-auto w-max min-w-full justify-start gap-1 rounded-xl border p-1 shadow-xs'>
        {items.map((item) => (
          <TabsTrigger
            key={item.id}
            value={item.id}
            className='data-[state=active]:bg-primary/10 data-[state=active]:text-primary dark:data-[state=active]:bg-primary/15 h-9 flex-none rounded-lg px-3 data-[state=active]:shadow-none'
          >
            {item.title}
          </TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}
