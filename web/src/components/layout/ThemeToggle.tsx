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
  const [focusIndex, setFocusIndex] = useState(() => choices.findIndex((choice) => choice.value === theme))
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const options = useRef<Array<HTMLButtonElement | null>>([])

  useEffect(() => {
    if (!open) return
    const selected = choices.findIndex((choice) => choice.value === theme)
    setFocusIndex(selected)
    options.current[selected]?.focus()
  }, [open, theme])

  useEffect(() => {
    if (!open) return
    const close = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    window.addEventListener('pointerdown', close)
    return () => window.removeEventListener('pointerdown', close)
  }, [open])

  const closeAndRestoreFocus = () => {
    setOpen(false)
    trigger.current?.focus()
  }

  const handleMenuKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    const current = options.current.indexOf(document.activeElement as HTMLButtonElement)
    let next = current
    if (event.key === 'ArrowDown') next = (current + 1) % choices.length
    else if (event.key === 'ArrowUp') next = (current - 1 + choices.length) % choices.length
    else if (event.key === 'Home') next = 0
    else if (event.key === 'End') next = choices.length - 1
    else if (event.key === 'Escape') {
      event.preventDefault()
      closeAndRestoreFocus()
      return
    } else return
    event.preventDefault()
    setFocusIndex(next)
    options.current[next]?.focus()
  }

  return (
    <div className="theme-control" ref={root}>
      <button
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label={`Tema: ${theme === 'system' ? 'sistema' : resolvedTheme === 'dark' ? 'escuro' : 'claro'}`}
        className="icon-button"
        onClick={() => setOpen((value) => !value)}
        ref={trigger}
        type="button"
      >
        {resolvedTheme === 'dark' ? <Moon aria-hidden="true" /> : <Sun aria-hidden="true" />}
      </button>
      {open ? (
        <div aria-label="Escolher tema" className="theme-menu" onKeyDown={handleMenuKeyDown} role="menu">
          {choices.map((choice, index) => (
            <button
              aria-checked={theme === choice.value}
              className="theme-option"
              key={choice.value}
              onClick={() => { setTheme(choice.value); closeAndRestoreFocus() }}
              ref={(element) => { options.current[index] = element }}
              role="menuitemradio"
              tabIndex={focusIndex === index ? 0 : -1}
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
