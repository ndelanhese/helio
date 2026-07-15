import { AlertTriangle, CheckCircle2 } from 'lucide-react'

import type { LiveSnapshot } from '../../api/types'

function joinCodes(codes: number[]) {
  if (codes.length < 2) return String(codes[0])
  if (codes.length === 2) return `${codes[0]} e ${codes[1]}`
  return `${codes.slice(0, -1).join(', ')} e ${codes.at(-1)}`
}

export function HealthPanel({ snapshot }: { snapshot: LiveSnapshot }) {
  const fault = snapshot.faultCodes.length > 0 || snapshot.status === 'fault'
  return (
    <section className={`health-panel ${fault ? 'has-fault' : ''}`} aria-labelledby="health-title">
      <div className="panel-kicker">{fault ? <AlertTriangle aria-hidden="true" /> : <CheckCircle2 aria-hidden="true" />}<span>Saúde do sistema</span></div>
      <h2 id="health-title">{fault ? 'Falha crítica' : 'Sistema operando normalmente'}</h2>
      {fault
        ? <p>Códigos {joinCodes(snapshot.faultCodes)}. A geração medida permanece visível para diagnóstico.</p>
        : <p>O inversor não relata falhas ativas nesta leitura.</p>}
    </section>
  )
}
