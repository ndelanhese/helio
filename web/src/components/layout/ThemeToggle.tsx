import { Check, Moon, Sun } from 'lucide-react'
import { useEffect, useRef, useState } from 'react'

import { type ThemeChoice, useTheme } from '../../app/theme'

const choices: Array<{ label: string; value: ThemeChoice }> = [
  { label: 'Sistema', value: 'system' },
  { label: 'Claro', value: 'light' },
  { label: 'Escuro', value: 'dark' },
]

export function ThemeToggle() {
  const { resolvedTheme, setTheme, theme } = useTheme()
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  }, [open])

  return (
    <div className="theme-control" ref={root}>
      <button
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label={`Tema: ${theme === 'system' ? 'sistema' : resolvedTheme === 'dark' ? 'escuro' : 'claro'}`}
        className="icon-button"
        onClick={() => setOpen((value) => !value)}
        type="button"
      >
        {resolvedTheme === 'dark' ? <Moon aria-hidden="true" /> : <Sun aria-hidden="true" />}
      </button>
      {open ? (
        <div aria-label="Escolher tema" className="theme-menu" role="menu">
          {choices.map((choice) => (
            <button
              aria-checked={theme === choice.value}
              className="theme-option"
              key={choice.value}
              onClick={() => { setTheme(choice.value); setOpen(false) }}
              role="menuitemradio"
              type="button"
            >
              <span>{choice.label}</span>
              {theme === choice.value ? <Check aria-hidden="true" /> : null}
            </button>
          ))}
        </div>
      ) : null}
    </div>
  )
}
