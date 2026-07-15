import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, useSyncExternalStore } from 'react'

import { LIVE_EVENTS_URL } from '../../api/live-events'
import { liveQuery, queryKeys } from '../../api/queries'
import type { LiveSnapshot, LiveState } from '../../api/types'
import type { ConnectionState } from '../../components/layout/ConnectionBadge'

type StreamState = 'connected' | 'reconnecting'
type EventWithState = { kind: 'snapshot'; snapshot: LiveSnapshot; state: LiveState; version?: 1 }

let sharedConnection: ConnectionState = 'unavailable'
const connectionListeners = new Set<() => void>()

function publishConnection(value: ConnectionState) {
  if (sharedConnection === value) return
  sharedConnection = value
  for (const listener of connectionListeners) listener()
}

export function useLiveConnectionState(): ConnectionState {
  return useSyncExternalStore(
    (listener) => { connectionListeners.add(listener); return () => connectionListeners.delete(listener) },
    () => sharedConnection,
    (): ConnectionState => 'unavailable',
  )
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function isSnapshot(value: unknown): value is LiveSnapshot {
  if (!isRecord(value) || !isRecord(value.pv1) || !isRecord(value.pv2) || !isRecord(value.grid)) return false
  return typeof value.observedAt === 'string' && typeof value.status === 'string'
    && typeof value.acPowerW === 'number' && typeof value.energyTodayWh === 'number'
    && typeof value.energyLifetimeWh === 'number' && Array.isArray(value.faultCodes)
    && typeof value.pv1.active === 'boolean' && typeof value.pv1.powerW === 'number'
    && typeof value.pv2.active === 'boolean' && typeof value.grid.voltageV === 'number'
    && typeof value.grid.frequencyHz === 'number'
}

function isLiveState(value: unknown): value is LiveState {
  if (!isRecord(value) || typeof value.stale !== 'boolean') return false
  if ('version' in value && value.version !== 1) return false
  return value.snapshot === undefined || isSnapshot(value.snapshot)
}

function parseData(value: string): unknown {
  try { return JSON.parse(value) }
  catch { return null }
}

function parseSnapshotEvent(value: string): EventWithState | null {
  const event = parseData(value)
  if (!isRecord(event) || event.kind !== 'snapshot' || !isLiveState(event.state) || !isSnapshot(event.snapshot)) return null
  if ('version' in event && event.version !== 1) return null
  return event as unknown as EventWithState
}

export function useLiveTelemetry() {
  const client = useQueryClient()
  const query = useQuery(liveQuery)
  const [streamState, setStreamState] = useState<StreamState>('connected')
  const [clock, setClock] = useState(() => Date.now())
  const [offline, setOffline] = useState(() => typeof navigator !== 'undefined' && !navigator.onLine)

  useEffect(() => {
    const interval = window.setInterval(() => setClock(Date.now()), 1_000)
    return () => window.clearInterval(interval)
  }, [])

  useEffect(() => {
    const online = () => setOffline(false)
    const offlineNow = () => setOffline(true)
    window.addEventListener('online', online)
    window.addEventListener('offline', offlineNow)
    return () => {
      window.removeEventListener('online', online)
      window.removeEventListener('offline', offlineNow)
    }
  }, [])

  useEffect(() => {
    if (typeof EventSource === 'undefined') return
    const source = new EventSource(LIVE_EVENTS_URL)
    const applyState = (message: Event) => {
      const parsed = parseData((message as MessageEvent).data)
      if (isLiveState(parsed)) client.setQueryData(queryKeys.live, parsed)
    }
    const applySnapshot = (message: Event) => {
      const parsed = parseSnapshotEvent((message as MessageEvent).data)
      if (!parsed) return
      client.setQueryData<LiveState>(queryKeys.live, { ...parsed.state, snapshot: parsed.snapshot })
    }
    source.addEventListener('state', applyState)
    source.addEventListener('snapshot', applySnapshot)
    source.onopen = () => {
      setStreamState('connected')
      void client.refetchQueries({ queryKey: queryKeys.live, type: 'active' })
    }
    source.onerror = () => {
      setStreamState('reconnecting')
      void client.refetchQueries({ queryKey: queryKeys.live, type: 'active' })
    }
    return () => {
      source.removeEventListener('state', applyState)
      source.removeEventListener('snapshot', applySnapshot)
      source.close()
    }
  }, [client])

  const lastSuccessAt = query.data?.lastSuccess ? Date.parse(query.data.lastSuccess) : Number.NaN
  const isOld = Number.isFinite(lastSuccessAt) && clock - lastSuccessAt > 30_000
  let connectionState: ConnectionState = 'connected'
  if (offline) connectionState = 'offline'
  else if (query.isPending) connectionState = 'loading'
  else if (!query.data?.snapshot && query.isError) connectionState = 'unavailable'
  else if (streamState === 'reconnecting') connectionState = 'reconnecting'
  else if (query.data?.stale || isOld) connectionState = 'stale'

  useEffect(() => {
    publishConnection(connectionState)
    return () => publishConnection('unavailable')
  }, [connectionState])

  return { ...query, connectionState, lastSuccessAgeMs: Number.isFinite(lastSuccessAt) ? Math.max(0, clock - lastSuccessAt) : null }
}
