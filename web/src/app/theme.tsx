import { createContext, type ReactNode, useContext, useEffect, useLayoutEffect, useMemo, useState } from 'react'

export type ThemeChoice = 'system' | 'light' | 'dark'
type ResolvedTheme = Exclude<ThemeChoice, 'system'>

const STORAGE_KEY = 'helio.theme.v1'
const DARK_QUERY = '(prefers-color-scheme: dark)'

interface ThemeContextValue {
  resolvedTheme: ResolvedTheme
  setTheme: (theme: ThemeChoice) => void
  theme: ThemeChoice
}

const ThemeContext = createContext<ThemeContextValue | null>(null)

function storedTheme(): ThemeChoice {
  const value = localStorage.getItem(STORAGE_KEY)
  return value === 'light' || value === 'dark' || value === 'system' ? value : 'system'
}

function systemTheme(): ResolvedTheme {
  return window.matchMedia(DARK_QUERY).matches ? 'dark' : 'light'
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemeChoice>(storedTheme)
  const [resolvedTheme, setResolvedTheme] = useState<ResolvedTheme>(() => theme === 'system' ? systemTheme() : theme)

  useEffect(() => {
    const query = window.matchMedia(DARK_QUERY)
    const resolve = () => setResolvedTheme(theme === 'system' ? (query.matches ? 'dark' : 'light') : theme)
    resolve()
    if (theme !== 'system') return
    query.addEventListener('change', resolve)
    return () => query.removeEventListener('change', resolve)
  }, [theme])

  useLayoutEffect(() => {
    document.documentElement.dataset.theme = resolvedTheme
    document.documentElement.style.colorScheme = resolvedTheme
  }, [resolvedTheme])

  const value = useMemo<ThemeContextValue>(() => ({
    resolvedTheme,
    setTheme: (nextTheme) => {
      localStorage.setItem(STORAGE_KEY, nextTheme)
      setThemeState(nextTheme)
    },
    theme,
  }), [resolvedTheme, theme])

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const value = useContext(ThemeContext)
  if (!value) throw new Error('useTheme must be used within ThemeProvider')
  return value
}
