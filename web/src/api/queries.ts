import { queryOptions } from '@tanstack/react-query'

import { ApiClient, api, authMemory } from './client'
import type { AlertsResponse, AlertState, AuthCredentials, BootstrapPayload, BootstrapStatus, ComponentHealth, InsightsResponse, LiveState, LoginPayload, Session, Settings } from './types'

const rootApi = new ApiClient('')

export const queryKeys = {
  bootstrap: ['bootstrap'] as const,
  health: ['health'] as const,
  history: (range?: string) => ['history', range] as const,
  insights: (day?: string) => ['insights', day] as const,
  alerts: (state: AlertState) => ['alerts', state] as const,
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
  queryFn: async ({ signal }) => {
    const session = await api.request<Session>('/auth/session', { signal })
    authMemory.setCSRFToken(session.csrfToken)
    return session
  },
})

export const liveQuery = queryOptions({
  queryKey: queryKeys.live,
  queryFn: ({ signal }) => api.request<LiveState>('/live', { signal }),
})

export const settingsQuery = queryOptions({
  queryKey: queryKeys.settings,
  queryFn: ({ signal }) => api.request<Settings>('/settings', { signal }),
})

export function insightsQuery(day: string) {
  return queryOptions({
    enabled: day.length > 0,
    queryKey: queryKeys.insights(day),
    queryFn: ({ signal }) => api.request<InsightsResponse>(`/insights?day=${encodeURIComponent(day)}`, { signal }),
  })
}

export function alertsQuery(state: AlertState, enabled = true) {
	return queryOptions({
		enabled,
    queryKey: queryKeys.alerts(state),
    queryFn: ({ signal }) => api.request<AlertsResponse>(`/alerts?state=${state}`, { signal }),
  })
}

export const componentHealthQuery = queryOptions({
  queryKey: queryKeys.health,
  queryFn: ({ signal }) => rootApi.request<ComponentHealth>('/health/components', { signal }),
  refetchInterval: 60_000,
})

export function login(payload: LoginPayload) {
  return api.request<AuthCredentials>('/auth/login', { method: 'POST', body: payload })
}

export function createBootstrap(payload: BootstrapPayload) {
  return api.request<AuthCredentials>('/bootstrap', { method: 'POST', body: payload })
}

export function updateSettings(payload: Settings) {
  const { installedPowerW: _derived, ...settings } = payload
  return api.request<Settings>('/settings', { method: 'PUT', body: settings })
}

export function downloadBackup() {
  return api.download('/data/backup')
}
