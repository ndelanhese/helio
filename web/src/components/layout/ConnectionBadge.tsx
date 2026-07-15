export type ConnectionState = 'connected' | 'loading' | 'offline' | 'reconnecting' | 'stale' | 'unavailable'

const labels: Record<ConnectionState, string> = {
  connected: 'Ao vivo',
  loading: 'Verificando dados',
  offline: 'Sem conexão',
  reconnecting: 'Reconectando',
  stale: 'Dados desatualizados',
  unavailable: 'Dados indisponíveis',
}

export function ConnectionBadge({ state }: { state: ConnectionState }) {
  return <span aria-live="polite" className={`connection-badge is-${state}`}><i aria-hidden="true" />{labels[state]}</span>
}
