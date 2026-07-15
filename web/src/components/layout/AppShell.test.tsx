import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ThemeProvider } from '../../app/theme'
import { AppShell } from './AppShell'

describe('AppShell', () => {
  it('marks current destination and keeps mobile targets accessible', () => {
    render(<ThemeProvider><AppShell currentPath="/history" /></ThemeProvider>)

    expect(screen.getByRole('link', { name: 'Histórico' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('navigation', { name: 'Principal' })).toBeVisible()
  })
})
