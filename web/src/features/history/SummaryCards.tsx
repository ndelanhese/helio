import type { HistorySummary } from './history-model'

const number = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2, minimumFractionDigits: 2 })
const coverageNumber = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 })

export function formatEnergy(wh: number) {
  return wh >= 1000 ? `${number.format(wh / 1000)} kWh` : `${number.format(wh)} Wh`
}

export function formatPower(watts: number) {
  return watts >= 1000 ? `${number.format(watts / 1000)} kW` : `${number.format(watts)} W`
}

function formatDuration(minutes: number) {
  const roundedMinutes = Math.round(minutes)
  const hours = Math.floor(roundedMinutes / 60)
  const rest = roundedMinutes % 60
  if (hours === 0) return `${rest} min`
  if (rest === 0) return `${hours} h`
  return `${hours} h ${rest} min`
}

export function SummaryCards({ current, previous }: { current: HistorySummary; previous?: HistorySummary }) {
  const comparison = previous && previous.energyWh > 0
    ? ((current.energyWh - previous.energyWh) / previous.energyWh) * 100
    : null
  const previousCoverage = previous?.coveragePct
  const coverageDiffers = current.coveragePct !== null && previousCoverage !== undefined && previousCoverage !== null
    && current.coveragePct !== previousCoverage

  return (
    <section aria-label="Resumo do período" className="history-summary">
      <dl>
        <div className="summary-energy"><dt>Energia observada</dt><dd>{formatEnergy(current.energyWh)}</dd></div>
        <div><dt>Pico observado</dt><dd>{formatPower(current.peakPowerW)}</dd></div>
        <div><dt>Tempo produtivo</dt><dd>{formatDuration(current.productiveMinutes)}</dd></div>
        <div><dt>Cobertura</dt><dd>{current.coveragePct === null ? 'Indisponível' : `${coverageNumber.format(current.coveragePct)}%`}</dd></div>
      </dl>
      <p className="comparison-copy">
        {comparison === null
          ? 'Comparação com o período anterior indisponível.'
          : `${comparison >= 0 ? '+' : ''}${number.format(comparison)}% em relação ao período anterior.`}
        {coverageDiffers && ' Os períodos têm cobertura diferente; compare com cautela.'}
      </p>
    </section>
  )
}
