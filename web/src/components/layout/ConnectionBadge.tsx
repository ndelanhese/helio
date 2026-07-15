export type ConnectionState = 'connected' | 'loading' | 'offline' | 'reconnecting' | 'stale' | 'unavailable'

const labels: Record<ConnectionState, string> = {
  connected: 'Ao vivo',
  loading: 'Verificando dados',
  offline: 'Sem conexão',
  reconnecting: 'Reconectando',
  stale: 'Dados desatualizados',
  unavailable: 'Dados indisponíveis',
}

export function ConnectionBadge({ announcement, state }: { announcement?: string; state: ConnectionState }) {
  return (
    <span className={`connection-badge is-${state}`}>
      <i aria-hidden="true" />{labels[state]}
      {announcement && <span aria-atomic="true" aria-live="polite" className="sr-only">{announcement}</span>}
    </span>
  )
}
