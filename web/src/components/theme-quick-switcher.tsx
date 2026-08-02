import { motion } from 'motion/react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { ATELIER_THEME_META } from '@/lib/atelier-themes'
import { useTheme } from '@/context/theme-provider'
import { Button } from '@/components/ui/button'

export function ThemeQuickSwitcher() {
  const { t } = useTranslation()
  const { theme, setTheme } = useTheme()

  return (
    <div className='px-2 pt-1.5 pb-1'>
      <div className='flex w-full items-center justify-between gap-3'>
        <span
          id='theme-switcher-label'
          className='text-muted-foreground text-sm select-none'
        >
          {t('Theme')}
        </span>
        <div
          role='radiogroup'
          aria-labelledby='theme-switcher-label'
          className='border-muted/50 bg-muted/40 inline-flex w-auto items-center gap-1 rounded-full border px-1.5 py-1'
        >
          {ATELIER_THEME_META.map((meta) => (
            <Button
              key={meta.value}
              variant='ghost'
              size='icon'
              role='radio'
              aria-label={meta.label}
              aria-checked={theme === meta.value}
              onClick={() => setTheme(meta.value)}
              className={cn(
                'relative size-7 rounded-full',
                theme === meta.value && 'text-accent-foreground'
              )}
            >
              {theme === meta.value && (
                <motion.span
                  layoutId='theme-switcher-active'
                  className='bg-accent ring-border absolute inset-0 rounded-full ring-1'
                  transition={{
                    type: 'spring',
                    stiffness: 500,
                    damping: 30,
                    mass: 0.2,
                  }}
                />
              )}
              <span
                className='relative z-10 inline-block size-3 rounded-full ring-1 ring-black/10'
                style={{ backgroundColor: meta.swatchColor }}
              />
            </Button>
          ))}
        </div>
      </div>
    </div>
  )
}
