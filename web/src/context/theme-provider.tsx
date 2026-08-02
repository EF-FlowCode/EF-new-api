import { createContext, useContext, useEffect, useState } from 'react'
import { getCookie, setCookie, removeCookie } from '@/lib/cookies'
import { THEMES, type Theme } from './theme-constants'

const DEFAULT_THEME: Theme = 'cinnabar'
const THEME_COOKIE_NAME = 'atelier-theme'
const THEME_COOKIE_MAX_AGE = 60 * 60 * 24 * 365

const isTheme = (v: unknown): v is Theme =>
  typeof v === 'string' && (THEMES as readonly string[]).includes(v)

type ThemeProviderProps = {
  children: React.ReactNode
  defaultTheme?: Theme
  storageKey?: string
}

type ThemeProviderState = {
  defaultTheme: Theme
  resolvedTheme: Theme
  theme: Theme
  setTheme: (theme: Theme) => void
  resetTheme: () => void
}

const initialState: ThemeProviderState = {
  defaultTheme: DEFAULT_THEME,
  resolvedTheme: DEFAULT_THEME,
  theme: DEFAULT_THEME,
  setTheme: () => null,
  resetTheme: () => null,
}

const ThemeContext = createContext<ThemeProviderState>(initialState)

export function ThemeProvider({
  children,
  defaultTheme = DEFAULT_THEME,
  storageKey = THEME_COOKIE_NAME,
  ...props
}: ThemeProviderProps) {
  const [theme, _setTheme] = useState<Theme>(() => {
    const saved = getCookie(storageKey)
    return isTheme(saved) ? saved : defaultTheme
  })

  useEffect(() => {
    const root = window.document.documentElement
    root.classList.remove('light', 'dark')
    root.setAttribute('data-theme', theme)
    // eflow: midnight(夜阑) 是全套唯一暗档，需要在 html 上加 .dark 让 tailwind 的
    // `dark:` 工具类、各处依赖暗色模式的组件（图表、弹层透明度等）都被激活。
    // 其余 5 档（含 frost 凝霜）保持亮色态。
    if (theme === 'midnight') {
      root.classList.add('dark')
    }
  }, [theme])

  const setTheme = (next: Theme) => {
    setCookie(storageKey, next, THEME_COOKIE_MAX_AGE)
    _setTheme(next)
  }

  const resetTheme = () => {
    removeCookie(storageKey)
    _setTheme(defaultTheme)
  }

  const contextValue: ThemeProviderState = {
    defaultTheme,
    resolvedTheme: theme,
    resetTheme,
    theme,
    setTheme,
  }

  return (
    <ThemeContext value={contextValue} {...props}>
      {children}
    </ThemeContext>
  )
}

// eslint-disable-next-line react-refresh/only-export-components
export const useTheme = () => {
  const context = useContext(ThemeContext)
  if (!context) throw new Error('useTheme must be used within a ThemeProvider')
  return context
}
