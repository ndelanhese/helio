import { queryOptions } from '@tanstack/react-query'

import { api } from './client'
import type { BootstrapStatus, LiveTelemetry, Session, Settings } from './types'

export const queryKeys = {
  bootstrap: ['bootstrap'] as const,
  health: ['health'] as const,
  history: (range?: string) => ['history', range] as const,
  insights: ['insights'] as const,
  live: ['live'] as const,
  session: ['auth', 'session'] as const,
  settings: ['settings'] as const,
}

export const bootstrapStatusQuery = queryOptions({
  queryKey: queryKeys.bootstrap,
  queryFn: ({ signal }) => api.request<BootstrapStatus>('/bootstrap/status', { signal }),
})

export const sessionQuery = queryOptions({
  queryKey: queryKeys.session,
  queryFn: ({ signal }) => api.request<Session>('/auth/session', { signal }),
})

export const liveQuery = queryOptions({
  queryKey: queryKeys.live,
  queryFn: ({ signal }) => api.request<LiveTelemetry>('/live', { signal }),
})

export const settingsQuery = queryOptions({
  queryKey: queryKeys.settings,
  queryFn: ({ signal }) => api.request<Settings>('/settings', { signal }),
})
