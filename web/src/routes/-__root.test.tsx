import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createMemoryHistory, RouterProvider } from '@tanstack/react-router'
import { render, screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { replaceLocation } from '../app/navigation'
import { createAppRouter } from '../app/router'
import { ThemeProvider } from '../app/theme'
import { server } from '../test/server'

vi.mock('../app/navigation', () => ({ replaceLocation: vi.fn() }))

function renderRoot(sessionStatus: number) {
  server.use(
    http.get('/api/v1/bootstrap/status', () => HttpResponse.json({ open: false })),
    http.get('/api/v1/auth/session', () => HttpResponse.json(
      { error: { code: sessionStatus === 401 ? 'unauthorized' : 'unavailable', message: 'failure' } },
      { status: sessionStatus },
    )),
  )
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const router = createAppRouter(createMemoryHistory({ initialEntries: ['/'] }))
  render(
    <QueryClientProvider client={client}>
      <ThemeProvider><RouterProvider router={router} /></ThemeProvider>
    </QueryClientProvider>,
  )
}

describe('RootLayout session gate', () => {
  afterEach(() => vi.mocked(replaceLocation).mockClear())

  it('keeps a transient session failure visible without redirecting', async () => {
    renderRoot(503)
    expect(await screen.findByText('Não foi possível verificar sua sessão.')).toBeVisible()
    expect(screen.getByRole('button', { name: 'Tentar novamente' })).toBeVisible()
    expect(replaceLocation).not.toHaveBeenCalled()
  })

  it('redirects a typed unauthorized response to login', async () => {
    renderRoot(401)
    await vi.waitFor(() => expect(replaceLocation).toHaveBeenCalledWith('/login'))
  })
})
