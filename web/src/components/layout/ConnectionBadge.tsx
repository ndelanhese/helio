export type ConnectionState = 'connected' | 'offline' | 'reconnecting' | 'stale'

const labels: Record<ConnectionState, string> = {
  connected: 'Ao vivo',
  offline: 'Sem conexão',
  reconnecting: 'Reconectando',
  stale: 'Dados desatualizados',
}

export function ConnectionBadge({ state = 'connected' }: { state?: ConnectionState }) {
  return <span aria-live="polite" className={`connection-badge is-${state}`}><i aria-hidden="true" />{labels[state]}</span>
}
