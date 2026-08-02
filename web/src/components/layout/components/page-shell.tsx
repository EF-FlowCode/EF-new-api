import { AppHeader } from './app-header'
import { Main } from './main'

type PageShellProps = {
  children: React.ReactNode
  /**
   * 是否把内容包进一个居中的容器。
   * 自带全幅布局的页面（如模型广场）传 false。
   * @default true
   */
  contained?: boolean
}

/**
 * eflow: 统一壳改造后的页面外壳。
 * 公开页 / 内容页统一用它：顶部工具条 + 可滚动主区。
 * 替代了原来的 PublicLayout —— 不再有独立的"营销站壳"。
 */
export function PageShell({ children, contained = true }: PageShellProps) {
  return (
    <>
      <AppHeader />
      <Main className='overflow-y-auto'>
        {contained ? (
          <div className='container mx-auto px-4 py-6'>{children}</div>
        ) : (
          children
        )}
      </Main>
    </>
  )
}
