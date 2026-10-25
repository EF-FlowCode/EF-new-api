import {
  Children,
  isValidElement,
  useState,
  type ReactElement,
  type ReactNode,
} from 'react'
import { useLocation } from '@tanstack/react-router'
import { AppHeader } from './app-header'
import { Main } from './main'
import { PageFooterProvider } from './page-footer'

type SlotProps = { children?: ReactNode }

function SectionPageLayoutTitle(_props: SlotProps) {
  return null
}
SectionPageLayoutTitle.displayName = 'SectionPageLayout.Title'

function SectionPageLayoutDescription(_props: SlotProps) {
  return null
}
SectionPageLayoutDescription.displayName = 'SectionPageLayout.Description'

function SectionPageLayoutActions(_props: SlotProps) {
  return null
}
SectionPageLayoutActions.displayName = 'SectionPageLayout.Actions'

function SectionPageLayoutContent(_props: SlotProps) {
  return null
}
SectionPageLayoutContent.displayName = 'SectionPageLayout.Content'

function SectionPageLayoutBreadcrumb(_props: SlotProps) {
  return null
}
SectionPageLayoutBreadcrumb.displayName = 'SectionPageLayout.Breadcrumb'

export type SectionPageLayoutProps = {
  children: ReactNode
}

export function SectionPageLayout(props: SectionPageLayoutProps) {
  const [footerContainer, setFooterContainer] = useState<HTMLDivElement | null>(
    null
  )
  const { pathname } = useLocation()
  const isAdminSurface =
    pathname.startsWith('/system-settings') ||
    /^\/(channels|users|models|redemption-codes|invitation-codes|admin-tickets|risk-center|usage-logs|keys)/.test(
      pathname
    )

  let title: ReactNode = null
  let description: ReactNode = null
  let actions: ReactNode = null
  let content: ReactNode = null
  let breadcrumb: ReactNode = null

  Children.forEach(props.children, (node) => {
    if (!isValidElement(node)) return
    const child = node as ReactElement<SlotProps>
    if (child.type === SectionPageLayoutTitle) title = child.props.children
    else if (child.type === SectionPageLayoutDescription)
      description = child.props.children
    else if (child.type === SectionPageLayoutActions)
      actions = child.props.children
    else if (child.type === SectionPageLayoutContent)
      content = child.props.children
    else if (child.type === SectionPageLayoutBreadcrumb)
      breadcrumb = child.props.children
  })

  return (
    <PageFooterProvider container={footerContainer}>
      <AppHeader />

      <Main>
        <div
          className={
            isAdminSurface
              ? 'bg-muted/20 shrink-0 border-b px-3 py-3 sm:px-5 sm:py-4'
              : 'shrink-0 px-3 pt-3 pb-2.5 sm:px-4 sm:pt-6 sm:pb-4'
          }
        >
          <div className='mx-auto w-full max-w-[1600px]'>
            {breadcrumb != null && <div className='mb-2'>{breadcrumb}</div>}
            <div className='flex flex-wrap items-center justify-between gap-x-3 gap-y-2 sm:gap-x-4'>
              <div className='min-w-0'>
                <div className='text-muted-foreground mb-1 text-[10px] font-semibold tracking-[0.18em] uppercase'>
                  {isAdminSurface ? 'Operations' : 'Workspace'}
                </div>
                <h2 className='truncate text-base font-bold tracking-tight sm:text-lg'>
                  {title}
                </h2>
                {description != null && (
                  <p className='text-muted-foreground line-clamp-2 max-sm:text-xs sm:text-sm'>
                    {description}
                  </p>
                )}
              </div>
              {actions != null && (
                <div className='flex shrink-0 flex-wrap items-center gap-1.5 sm:gap-2'>
                  {actions}
                </div>
              )}
            </div>
          </div>
        </div>

        <div className='min-h-0 flex-1 overflow-auto px-3 py-3 sm:px-5 sm:py-4'>
          <div className='mx-auto w-full max-w-[1600px]'>{content}</div>
        </div>

        <div
          ref={setFooterContainer}
          className='bg-background/95 shrink-0 border-t px-3 py-2.5 backdrop-blur empty:hidden sm:px-5 sm:py-3'
        />
      </Main>
    </PageFooterProvider>
  )
}

SectionPageLayout.Title = SectionPageLayoutTitle
SectionPageLayout.Description = SectionPageLayoutDescription
SectionPageLayout.Actions = SectionPageLayoutActions
SectionPageLayout.Content = SectionPageLayoutContent
SectionPageLayout.Breadcrumb = SectionPageLayoutBreadcrumb
