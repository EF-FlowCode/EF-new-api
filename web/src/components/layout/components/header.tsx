import { cn } from '@/lib/utils'
import { Separator } from '@/components/ui/separator'
import { SidebarTrigger } from '@/components/ui/sidebar'

type HeaderProps = React.HTMLAttributes<HTMLElement>

export function Header({ className, children, ...props }: HeaderProps) {
  return (
    <header
      className={cn(
        // eflow: 统一壳顶栏毛玻璃——半透明 + backdrop-blur
        'bg-background/70 z-50 h-16 shrink-0 border-b backdrop-blur-xl',
        className
      )}
      {...props}
    >
      <div className='flex h-full items-center gap-3 p-4 sm:gap-4'>
        <SidebarTrigger variant='outline' />
        <Separator orientation='vertical' className='h-6' />
        {children}
      </div>
    </header>
  )
}
