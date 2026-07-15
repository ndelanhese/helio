import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { ThemeProvider } from '../../app/theme'
import { AppShell } from './AppShell'

describe('AppShell', () => {
  it('marks current destination and keeps mobile targets accessible', () => {
    render(<ThemeProvider><AppShell connectionState="unavailable" currentPath="/history" /></ThemeProvider>)

    expect(screen.getByRole('link', { name: 'Histórico' })).toHaveAttribute('aria-current', 'page')
    expect(screen.getByRole('navigation', { name: 'Principal' })).toBeVisible()
    expect(screen.getByText('Dados indisponíveis')).toBeVisible()
    expect(screen.queryByText('Ao vivo')).not.toBeInTheDocument()
  })

  it('labels an offline connection without claiming live data', () => {
    render(<ThemeProvider><AppShell connectionState="offline" currentPath="/" /></ThemeProvider>)
    expect(screen.getByText('Sem conexão')).toBeVisible()
    expect(screen.queryByText('Ao vivo')).not.toBeInTheDocument()
  })

  it('uses one restrained polite surface for connection and fault announcements', () => {
    render(<ThemeProvider><AppShell announcement="Ao vivo. Falha informada pelo inversor. Nenhum código foi informado." connectionState="connected" currentPath="/" /></ThemeProvider>)
    const regions = screen.getAllByText(/Falha informada pelo inversor/)
    expect(regions).toHaveLength(1)
    expect(regions[0]).toHaveAttribute('aria-live', 'polite')
    expect(screen.getByText('Ao vivo', { selector: '.connection-badge' })).toBeVisible()
  })
})
