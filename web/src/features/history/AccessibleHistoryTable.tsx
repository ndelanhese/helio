import type { HistoryPoint } from './history-model'
import { toChartPoint } from './history-model'
import { formatPower } from './SummaryCards'
import { useState } from 'react'

export function AccessibleHistoryTable({ points, timezone }: { points: HistoryPoint[]; timezone: string }) {
  const [open, setOpen] = useState(false)
  const dates = new Intl.DateTimeFormat('pt-BR', {
    day: '2-digit', hour: '2-digit', hour12: false, minute: '2-digit', month: '2-digit', timeZone: timezone, year: 'numeric',
  })
  return (
    <details className="history-table-disclosure" onToggle={(event) => setOpen(event.currentTarget.open)}>
      <summary>Ver dados precisos</summary>
      {open && <div className="history-table-scroll">
        <table aria-label="Leituras do período atual">
          <thead><tr><th scope="col">Data e hora</th><th scope="col">Potência observada</th></tr></thead>
          <tbody>
            {points.map((point) => (
              <tr key={point.at}>
                <td>{dates.format(new Date(point.at))}</td>
                <td>{formatPower(toChartPoint(point).powerW)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>}
    </details>
  )
}
