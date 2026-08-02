import { Palette } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { ATELIER_THEME_META } from '@/lib/atelier-themes'
import { useTheme } from '@/context/theme-provider'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

export function ThemeSwitch() {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button variant='ghost' size='icon' className='h-9 w-9 rounded-full'>
          <Palette className='size-[1.2rem]' />
          <span className='sr-only'>{t('Toggle theme')}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        {ATELIER_THEME_META.map((meta) => (
          <DropdownMenuItem
            key={meta.value}
            onClick={() => setTheme(meta.value)}
          >
            <span
              className='mr-2 inline-block size-3 rounded-full ring-1 ring-black/10'
              style={{ backgroundColor: meta.swatchColor }}
            />
            {meta.label}
            {/* eflow: 选中态盖一枚「朱印」(印泥红方印) 代替对勾，作统一签名 */}
            {theme === meta.value && (
              <span
                aria-hidden
                className='ms-auto inline-flex h-[18px] w-[18px] items-center justify-center rounded-[4px] text-[11px] font-bold leading-none'
                style={{
                  background: 'var(--seal)',
                  color: '#f6efe1',
                  transform: 'rotate(-6deg)',
                  fontFamily: "'Noto Serif SC', serif",
                  boxShadow:
                    'inset 0 0 0 1px rgba(246,239,225,0.35), 0 1px 2px rgba(0,0,0,0.25)',
                }}
              >
                印
              </span>
            )}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
