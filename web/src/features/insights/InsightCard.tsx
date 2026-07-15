import { CircleGauge, History, Info } from 'lucide-react'
import { useState } from 'react'

import type { InsightsResponse } from '../../api/types'

const confidenceLabel = { low: 'Confiança baixa', medium: 'Confiança média', high: 'Confiança alta' } as const

function formatEvidence(value: number, unit: string) {
  const formatted = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2 }).format(value)
  const labels: Record<string, string> = { days: 'dias', percent: '%', hours: 'horas', ratio: '' }
  return `${formatted}${labels[unit] ? ` ${labels[unit]}` : unit ? ` ${unit}` : ''}`
}

export function InsightCard({ insight }: { insight: InsightsResponse }) {
	const [showModelNote, setShowModelNote] = useState(false)
  const insufficient = insight.observationWindow.qualifyingDays < insight.observationWindow.minimumDays
  const nonqualifying = !insight.qualifying
  const conclusion = nonqualifying
    ? 'Telemetria insuficiente para comparar'
    : insufficient
    ? 'Histórico ainda insuficiente'
    : insight.ratio < 0.65 ? 'Produção abaixo da referência aprendida' : 'Produção dentro da faixa observada'
  return (
    <section className="insight-card" aria-labelledby="insight-conclusion">
      <div className="insight-health"><CircleGauge aria-hidden="true" /><span>{nonqualifying ? 'Dia não qualificável' : insufficient ? 'Aprendendo a referência' : 'Análise diária'}</span></div>
      <h2 id="insight-conclusion">{conclusion}</h2>
      <span className={`confidence-pill is-${insight.confidence}`}>{confidenceLabel[insight.confidence]}</span>
      {nonqualifying ? <p className="insight-qualification-note">Este dia não reuniu dados qualificáveis para uma conclusão. A confiança e as evidências abaixo explicam a limitação da telemetria.</p> : null}
      <dl className="insight-measures">
        <div><dt>Energia medida</dt><dd>{new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2 }).format(insight.actualWh / 1000)} kWh</dd></div>
        <div><dt>Referência estimada <button aria-describedby={showModelNote ? 'insight-model-note' : undefined} aria-label="Como a referência estimada usa radiação solar" className="model-tooltip-trigger" onBlur={() => setShowModelNote(false)} onFocus={() => setShowModelNote(true)} onMouseEnter={() => setShowModelNote(true)} onMouseLeave={() => setShowModelNote(false)} type="button"><Info aria-hidden="true" /></button></dt><dd>{new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 2 }).format(insight.expectedWh / 1000)} kWh</dd>{showModelNote && <p className="model-tooltip" id="insight-model-note" role="tooltip">Referência combina histórico do sistema e radiação modelada. Não substitui medição no painel nem confirma falha isolada.</p>}</div>
      </dl>
      <div className="insight-evidence">
        <h3>Evidências da análise</h3>
        {insight.evidence.length > 0 ? <ul>{insight.evidence.map((item) => <li key={item.code}><span>{item.label}</span><strong>{formatEvidence(item.value, item.unit)}</strong></li>)}</ul> : <p>Nenhuma evidência adicional foi registrada para este dia.</p>}
      </div>
      <p className="observation-window"><History aria-hidden="true" />{insight.observationWindow.qualifyingDays} de {insight.observationWindow.minimumDays} dias qualificáveis na janela mínima.</p>
    </section>
  )
}
