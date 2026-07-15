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
})
