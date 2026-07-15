import { act, renderHook, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createElement, type ReactNode } from 'react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { LiveState } from '../../api/types'
import { queryKeys } from '../../api/queries'
import { useLiveStatus, useLiveTelemetry } from './useLiveTelemetry'

const state: LiveState = {
  lastSuccess: '2026-07-14T15:42:00Z',
  stale: false,
  snapshot: {
    observedAt: '2026-07-14T15:42:00Z', status: 'normal', acPowerW: 2070,
    energyTodayWh: 12340, energyLifetimeWh: 4567800,
    pv1: { active: true, voltageV: 267.1, currentA: 8, powerW: 2070 },
    pv2: { active: false, voltageV: 0, currentA: 0, powerW: 0 },
    grid: { voltageV: 267.1, frequencyHz: 59.97 }, faultCodes: [],
  },
}

class FakeEventSource {
  static instance: FakeEventSource
  onerror: ((event: Event) => void) | null = null
  onopen: ((event: Event) => void) | null = null
  listeners = new Map<string, (event: MessageEvent) => void>()
  close = vi.fn()
  constructor(public readonly url: string) { FakeEventSource.instance = this }
  addEventListener(type: string, listener: EventListener) { this.listeners.set(type, listener as (event: MessageEvent) => void) }
  removeEventListener(type: string) { this.listeners.delete(type) }
  emit(type: string, data: unknown) { this.listeners.get(type)?.(new MessageEvent(type, { data: JSON.stringify(data) })) }
}

describe('useLiveTelemetry', () => {
  let client: QueryClient
  let requests: number

  beforeEach(() => {
    vi.setSystemTime(new Date('2026-07-14T15:42:10Z'))
    requests = 0
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.stubGlobal('fetch', vi.fn(async () => {
      requests += 1
      return new Response(JSON.stringify(state), { headers: { 'Content-Type': 'application/json' } })
    }))
  })

  afterEach(() => vi.unstubAllGlobals())

  function wrapper({ children }: { children: ReactNode }) {
    return createElement(QueryClientProvider, { client }, children)
  }

  it('opens authenticated SSE and applies initial state and snapshot events to the live cache', async () => {
    const { result } = renderHook(() => useLiveTelemetry(), { wrapper })
    await waitFor(() => expect(result.current.data?.snapshot?.acPowerW).toBe(2070))
    expect(FakeEventSource.instance.url).toBe('/api/v1/live/events')
    expect(result.current.connectionState).toBe('loading')

    act(() => FakeEventSource.instance.onopen?.(new Event('open')))
    await waitFor(() => expect(result.current.connectionState).toBe('connected'))

    act(() => FakeEventSource.instance.emit('state', { ...state, stale: true }))
    expect(client.getQueryData<LiveState>(queryKeys.live)?.stale).toBe(true)

    act(() => FakeEventSource.instance.emit('snapshot', {
      kind: 'snapshot', state: { ...state, lastSuccess: '2026-07-14T15:42:10Z' },
      snapshot: { ...state.snapshot, acPowerW: 2210 },
    }))
    expect(client.getQueryData<LiveState>(queryKeys.live)?.snapshot?.acPowerW).toBe(2210)
  })

  it('marks reconnecting and refetches on stream error, then refetches on open', async () => {
    const status = renderHook(() => useLiveStatus())
    const { result } = renderHook(() => useLiveTelemetry(), { wrapper })
    await waitFor(() => expect(result.current.data).toBeDefined())
    act(() => FakeEventSource.instance.onopen?.(new Event('open')))
    await waitFor(() => expect(result.current.connectionState).toBe('connected'))
    const initialRequests = requests

    act(() => FakeEventSource.instance.onerror?.(new Event('error')))
    expect(result.current.connectionState).toBe('reconnecting')
    await waitFor(() => expect(status.result.current.connectionState).toBe('reconnecting'))
    expect(status.result.current.announcement).toBe('Reconectando')
    await waitFor(() => expect(requests).toBeGreaterThan(initialRequests))

    const afterError = requests
    act(() => FakeEventSource.instance.onopen?.(new Event('open')))
    await waitFor(() => expect(result.current.connectionState).toBe('connected'))
    await waitFor(() => expect(requests).toBeGreaterThan(afterError))
  })

  it('ignores malformed and unknown stream events', async () => {
    renderHook(() => useLiveTelemetry(), { wrapper })
    await waitFor(() => expect(client.getQueryData(queryKeys.live)).toBeDefined())
    const before = client.getQueryData(queryKeys.live)
    act(() => {
      FakeEventSource.instance.emit('snapshot', { kind: 'snapshot', state, snapshot: { ...state.snapshot, pv1: { active: true } } })
      FakeEventSource.instance.emit('unknown', state)
    })
    expect(client.getQueryData(queryKeys.live)).toEqual(before)
  })

  it('does not let an older HTTP response replace a newer SSE snapshot', async () => {
    let resolveRequest!: (response: Response) => void
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => { resolveRequest = resolve })))
    renderHook(() => useLiveTelemetry(), { wrapper })
    await waitFor(() => expect(FakeEventSource.instance).toBeDefined())
    act(() => FakeEventSource.instance.emit('snapshot', {
      kind: 'snapshot', state: { ...state, lastSuccess: '2026-07-14T15:42:20Z' },
      snapshot: { ...state.snapshot, observedAt: '2026-07-14T15:42:20Z', acPowerW: 2400 },
    }))
    resolveRequest(new Response(JSON.stringify(state), { headers: { 'Content-Type': 'application/json' } }))
    await act(async () => undefined)
    expect(client.getQueryData<LiveState>(queryKeys.live)?.snapshot?.acPowerW).toBe(2400)
  })

  it('reports offline transitions in Portuguese', async () => {
    const { result } = renderHook(() => useLiveTelemetry(), { wrapper })
    await waitFor(() => expect(result.current.data).toBeDefined())
    act(() => window.dispatchEvent(new Event('offline')))
    expect(result.current.connectionState).toBe('offline')
    expect(result.current.announcement).toMatch(/Sem conexão/)
  })

  it('closes the stream and removes listeners on unmount', async () => {
    const { unmount } = renderHook(() => useLiveTelemetry(), { wrapper })
    await waitFor(() => expect(FakeEventSource.instance).toBeDefined())
    const source = FakeEventSource.instance
    unmount()
    expect(source.close).toHaveBeenCalledOnce()
    expect(source.listeners.size).toBe(0)
  })
})
