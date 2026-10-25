import { useNavigate } from '@tanstack/react-router'
import { cn } from '@/lib/utils'

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
    <nav className='bg-background/95 border-border/70 sticky top-0 z-20 mb-4 flex min-w-0 gap-1 overflow-x-auto border-b py-1 backdrop-blur'>
      <span className='text-muted-foreground me-2 flex shrink-0 items-center px-1 text-[10px] font-semibold tracking-[0.16em] uppercase'>
        Settings
      </span>
      <div className='flex min-w-0 gap-1'>
        {items.map((item) => (
          <button
            key={item.id}
            type='button'
            onClick={() => handleChange(item.id)}
            className={cn(
              'text-muted-foreground hover:bg-muted hover:text-foreground flex h-8 shrink-0 items-center rounded-md px-3 text-left text-xs transition-colors',
              activeSection === item.id &&
                'bg-primary/10 text-primary font-semibold'
            )}
          >
            <span className='truncate'>{item.title}</span>
            {activeSection === item.id && (
              <span className='bg-primary ms-2 size-1.5 rounded-full' />
            )}
          </button>
        ))}
      </div>
    </nav>
  )
}
