import type { LiveSnapshot } from '../../api/types'

const pt = new Intl.NumberFormat('pt-BR')

export function formatPower(watts: number) {
  return watts < 1_000
    ? `${pt.format(Math.round(watts))} W`
    : `${new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(watts / 1_000)} kW`
}

export function formatEnergy(wattHours: number) {
  return wattHours < 1_000
    ? `${pt.format(Math.round(wattHours))} Wh`
    : `${new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(wattHours / 1_000)} kWh`
}

function statusLabel(status: string) {
  const labels: Record<string, string> = {
    checking: 'Verificando inversor', fault: 'Falha no inversor', normal: 'Operação normal', standby: 'Inversor em espera',
  }
  return labels[status] ?? 'Estado do inversor disponível'
}

export function HeroPower({ snapshot, updatedAt, timezone }: { snapshot: LiveSnapshot; updatedAt?: string; timezone?: string }) {
  const time = updatedAt
    ? new Intl.DateTimeFormat('pt-BR', { hour: '2-digit', minute: '2-digit', second: '2-digit', timeZone: timezone }).format(new Date(updatedAt))
    : null
  return (
    <section className="live-hero" aria-labelledby="power-now">
      <div className="live-hero-heading">
        <p className="eyebrow">{statusLabel(snapshot.status)}</p>
        <p className="live-updated tabular-nums">{time ? `Atualizado às ${time}` : 'Horário da leitura indisponível'}</p>
      </div>
      <div className="power-lockup">
        <p>Potência agora</p>
        <h1 id="power-now" className="metric">{formatPower(snapshot.acPowerW)}</h1>
      </div>
      <dl className="energy-totals">
        <div><dt>Gerado hoje</dt><dd className="metric">{formatEnergy(snapshot.energyTodayWh)}</dd></div>
        <div><dt>Desde a instalação</dt><dd className="metric">{formatEnergy(snapshot.energyLifetimeWh)}</dd></div>
      </dl>
    </section>
  )
}
