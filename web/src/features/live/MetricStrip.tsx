import type { LiveSnapshot } from '../../api/types'

const decimal = (value: number, digits: number) => new Intl.NumberFormat('pt-BR', {
  minimumFractionDigits: digits, maximumFractionDigits: digits,
}).format(value)

export function MetricStrip({ snapshot }: { snapshot: LiveSnapshot }) {
  return (
    <dl className="metric-strip" aria-label="Leituras elétricas">
      <div><dt>Tensão PV1</dt><dd className="metric">{decimal(snapshot.pv1.voltageV, 1)} V</dd></div>
      <div><dt>Corrente PV1</dt><dd className="metric">{decimal(snapshot.pv1.currentA, 2)} A</dd></div>
      <div><dt>Tensão da rede</dt><dd className="metric">{decimal(snapshot.grid.voltageV, 1)} V</dd></div>
      <div><dt>Frequência</dt><dd className="metric">{decimal(snapshot.grid.frequencyHz, 2)} Hz</dd></div>
    </dl>
  )
}
