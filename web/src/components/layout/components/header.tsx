import { cn } from '@/lib/utils'
import { Separator } from '@/components/ui/separator'
import { SidebarTrigger } from '@/components/ui/sidebar'

type HeaderProps = React.HTMLAttributes<HTMLElement>

export function Header({ className, children, ...props }: HeaderProps) {
  return (
    <header
      className={cn(
        // eflow: 统一壳顶栏毛玻璃——半透明 + backdrop-blur
        'bg-background/85 z-50 h-12 shrink-0 border-b backdrop-blur-xl',
        className
      )}
      {...props}
    >
      <div className='flex h-full items-center gap-2 px-3 sm:gap-3 sm:px-4'>
        <SidebarTrigger variant='outline' className='size-8' />
        <Separator orientation='vertical' className='h-5' />
        {children}
      </div>
    </header>
  )
}
