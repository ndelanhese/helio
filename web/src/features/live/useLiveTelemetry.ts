import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, useSyncExternalStore } from 'react'

import { LIVE_EVENTS_URL, parseLiveEvent } from '../../api/live-events'
import { liveQuery, queryKeys } from '../../api/queries'
import type { LiveSnapshot, LiveState } from '../../api/types'
import type { ConnectionState } from '../../components/layout/ConnectionBadge'

type StreamState = 'connecting' | 'connected' | 'reconnecting'
interface SharedLiveStatus { announcement: string; connectionState: ConnectionState }

const connectionLabels: Record<ConnectionState, string> = {
  connected: 'Ao vivo', loading: 'Verificando dados', offline: 'Sem conexão',
  reconnecting: 'Reconectando', stale: 'Dados desatualizados', unavailable: 'Dados indisponíveis',
}
let sharedStatus: SharedLiveStatus = { announcement: 'Dados indisponíveis', connectionState: 'unavailable' }
const statusListeners = new Set<() => void>()

function faultAnnouncement(snapshot?: LiveSnapshot) {
  if (!snapshot || (snapshot.status !== 'fault' && snapshot.faultCodes.length === 0)) return 'Sistema sem falhas ativas.'
  if (snapshot.faultCodes.length === 0) return 'Falha informada pelo inversor. Nenhum código foi informado.'
  return `Falha informada pelo inversor. Códigos ${snapshot.faultCodes.join(', ')}.`
}

function publishStatus(connectionState: ConnectionState, snapshot?: LiveSnapshot) {
  const fault = faultAnnouncement(snapshot)
  const announcement = connectionState === 'connected' ? `${connectionLabels[connectionState]}. ${fault}` : connectionLabels[connectionState]
  if (sharedStatus.connectionState === connectionState && sharedStatus.announcement === announcement) return
  sharedStatus = { announcement, connectionState }
  for (const listener of statusListeners) listener()
}

export function useLiveStatus(): SharedLiveStatus {
  return useSyncExternalStore(
    (listener) => { statusListeners.add(listener); return () => statusListeners.delete(listener) },
    () => sharedStatus,
    () => sharedStatus,
  )
}

function time(value?: string) {
  const parsed = value ? Date.parse(value) : Number.NaN
  return Number.isFinite(parsed) ? parsed : -Infinity
}

function mergeState(current: LiveState | undefined, incoming: LiveState, eventSnapshot?: LiveSnapshot): LiveState {
  if (!current) return { ...incoming, snapshot: eventSnapshot ?? incoming.snapshot }
  const candidate = eventSnapshot ?? incoming.snapshot
  const snapshot = time(candidate?.observedAt) >= time(current.snapshot?.observedAt) ? candidate : current.snapshot
  const incomingIsCurrent = time(incoming.lastSuccess) >= time(current.lastSuccess)
  return {
    ...(incomingIsCurrent ? current : incoming),
    ...(incomingIsCurrent ? incoming : current),
    lastSuccess: incomingIsCurrent ? (incoming.lastSuccess ?? current.lastSuccess) : current.lastSuccess,
    snapshot,
    stale: incomingIsCurrent ? incoming.stale : current.stale,
  }
}

export function useLiveTelemetry() {
  const client = useQueryClient()
  const query = useQuery(liveQuery)
  const [streamState, setStreamState] = useState<StreamState>('connecting')
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
    const apply = (kind: 'state' | 'snapshot', message: Event) => {
      const parsed = parseLiveEvent(kind, (message as MessageEvent).data)
      if (!parsed) return
      void client.cancelQueries({ queryKey: queryKeys.live })
      client.setQueryData<LiveState>(queryKeys.live, (current) => mergeState(
        current,
        parsed.state,
        parsed.kind === 'snapshot' ? parsed.snapshot : undefined,
      ))
      setStreamState('connected')
    }
    const applyState = (message: Event) => apply('state', message)
    const applySnapshot = (message: Event) => apply('snapshot', message)
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
  else if (!query.data?.snapshot && query.isError) connectionState = 'unavailable'
  else if (query.isPending || streamState === 'connecting') connectionState = 'loading'
  else if (streamState === 'reconnecting') connectionState = 'reconnecting'
  else if (query.data?.stale || isOld) connectionState = 'stale'

  const announcement = connectionState === 'connected'
    ? `${connectionLabels[connectionState]}. ${faultAnnouncement(query.data?.snapshot)}`
    : connectionLabels[connectionState]

  useEffect(() => publishStatus(connectionState, query.data?.snapshot), [connectionState, query.data?.snapshot])
  useEffect(() => () => publishStatus('unavailable'), [])

  return {
    ...query,
    announcement,
    connectionState,
    lastSuccessAgeMs: Number.isFinite(lastSuccessAt) ? Math.max(0, clock - lastSuccessAt) : null,
  }
}
