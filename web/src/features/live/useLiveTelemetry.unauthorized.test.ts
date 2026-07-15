import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { createElement, type ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'

import { configureUnauthorizedHandler } from '../../api/client'
import { useLiveTelemetry } from './useLiveTelemetry'

class QuietEventSource {
  onerror: ((event: Event) => void) | null = null
  onopen: ((event: Event) => void) | null = null
  addEventListener() {}
  removeEventListener() {}
  close() {}
}

afterEach(() => {
  configureUnauthorizedHandler(() => undefined)
  vi.unstubAllGlobals()
})

it('treats a 401 live response as unavailable and invokes the unauthorized handler', async () => {
  const unauthorized = vi.fn()
  configureUnauthorizedHandler(unauthorized)
  vi.stubGlobal('EventSource', QuietEventSource)
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({
    error: { code: 'unauthorized', message: 'Sessão expirada' },
  }), { status: 401, headers: { 'Content-Type': 'application/json' } })))
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => createElement(QueryClientProvider, { client }, children)
  const { result } = renderHook(() => useLiveTelemetry(), { wrapper })

  await waitFor(() => expect(result.current.isError).toBe(true))
  expect(result.current.connectionState).toBe('unavailable')
  expect(unauthorized).toHaveBeenCalledOnce()
})
