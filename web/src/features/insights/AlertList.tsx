import { AlertTriangle, CircleCheck } from 'lucide-react'

import type { AlertDTO, AlertState } from '../../api/types'

export function AlertList({ alerts, state }: { alerts: AlertDTO[]; state: AlertState }) {
  const open = state === 'open'
  return (
    <section className={`alert-list is-${state}`} aria-labelledby={`alerts-${state}`}>
      <div className="alert-list-heading">
        {open ? <AlertTriangle aria-hidden="true" /> : <CircleCheck aria-hidden="true" />}
        <div><p className="eyebrow">{open ? 'Acompanhamento' : 'Recuperação'}</p><h2 id={`alerts-${state}`}>{open ? 'Alertas ativos' : 'Recuperações recentes'}</h2></div>
      </div>
      {alerts.length === 0 ? <p className="alert-empty">{open ? 'Nenhum alerta ativo agora.' : 'Nenhuma recuperação recente registrada.'}</p> : <ol>
        {alerts.map((alert) => <li key={`${alert.kind}-${alert.openedAt}`}>
          <div><span className={`alert-state is-${alert.severity}`}>{open ? 'Ativo' : 'Resolvido'}</span><time dateTime={alert.resolvedAt ?? alert.openedAt}>{new Intl.DateTimeFormat('pt-BR', { dateStyle: 'medium' }).format(new Date(alert.resolvedAt ?? alert.openedAt))}</time></div>
          <h3>{alert.title}</h3><p>{alert.summary}</p>
          {alert.evidence.length > 0 && <ul>{alert.evidence.map((item) => <li key={`${item.label}-${item.unit}`}>{item.label}: {new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2 }).format(item.value)} {item.unit}</li>)}</ul>}
        </li>)}
      </ol>}
    </section>
  )
}
