import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { ThemeProvider } from '../../app/theme'
import { ThemeToggle } from './ThemeToggle'

function matchMediaMock(dark: boolean) {
  const listeners = new Set<(event: MediaQueryListEvent) => void>()
  vi.stubGlobal('matchMedia', vi.fn().mockImplementation(() => ({
    matches: dark,
    media: '(prefers-color-scheme: dark)',
    onchange: null,
    addEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: (event: MediaQueryListEvent) => void) => listeners.delete(listener),
    dispatchEvent: () => true,
  })))
  return listeners
}

describe('ThemeToggle', () => {
  it('follows system until user chooses a theme', async () => {
    matchMediaMock(false)
    render(<ThemeProvider><ThemeToggle /></ThemeProvider>)

    expect(document.documentElement.dataset.theme).toBe('light')
    await userEvent.click(screen.getByRole('button', { name: /tema/i }))
    await userEvent.click(screen.getByRole('menuitemradio', { name: 'Escuro' }))

    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(localStorage.getItem('helio.theme.v1')).toBe('dark')
  })

  it('supports roving keyboard focus and restores the trigger on Escape', async () => {
    matchMediaMock(false)
    render(<ThemeProvider><ThemeToggle /></ThemeProvider>)
    const trigger = screen.getByRole('button', { name: /tema/i })
    await userEvent.click(trigger)

    expect(screen.getByRole('menuitemradio', { name: 'Sistema' })).toHaveFocus()
    expect(screen.getByRole('menuitemradio', { name: 'Sistema' })).toHaveAttribute('tabindex', '0')
    expect(screen.getByRole('menuitemradio', { name: 'Escuro' })).toHaveAttribute('tabindex', '-1')
    await userEvent.keyboard('{ArrowDown}{End}')
    expect(screen.getByRole('menuitemradio', { name: 'Escuro' })).toHaveFocus()
    expect(screen.getByRole('menuitemradio', { name: 'Escuro' })).toHaveAttribute('tabindex', '0')
    await userEvent.keyboard('{Home}{Escape}')
    expect(trigger).toHaveFocus()
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })

  it('survives blocked storage and listens to the system only in system mode', async () => {
    const listeners = matchMediaMock(false)
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('blocked') })
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('blocked') })
    render(<ThemeProvider><ThemeToggle /></ThemeProvider>)

    expect(document.documentElement.dataset.theme).toBe('light')
    expect(listeners.size).toBe(1)
    await userEvent.click(screen.getByRole('button', { name: /tema/i }))
    await userEvent.click(screen.getByRole('menuitemradio', { name: 'Escuro' }))
    expect(document.documentElement.dataset.theme).toBe('dark')
    expect(setItem).toHaveBeenCalled()
    expect(listeners.size).toBe(0)
  })
})
