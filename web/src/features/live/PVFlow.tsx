import { ArrowRight, Cable, SunMedium, UtilityPole } from 'lucide-react'

import type { LiveSnapshot, Settings } from '../../api/types'
import { formatPower } from './HeroPower'

export function PVFlow({ snapshot, settings }: { snapshot: LiveSnapshot; settings?: Settings }) {
  const capacity = settings ? formatPower(settings.panelCount * settings.panelWattage).replace('kW', 'kWp') : null
  return (
    <section className="pv-flow" aria-labelledby="flow-title">
      <div className="section-heading">
        <p className="eyebrow">Caminho da geração</p>
        <h2 id="flow-title">Do sol à rede elétrica</h2>
      </div>
      <ol className="flow-line">
        <li>
          <SunMedium aria-hidden="true" />
          <div className="flow-channels">
            <div><span>PV1</span><strong className="metric">{formatPower(snapshot.pv1.powerW)}</strong></div>
            {snapshot.pv2.active && <div><span>PV2</span><strong className="metric">{formatPower(snapshot.pv2.powerW)}</strong></div>}
          </div>
          <ArrowRight aria-hidden="true" className="flow-arrow" />
        </li>
        <li>
          <Cable aria-hidden="true" />
          <div><span>Inversor</span><strong>{snapshot.status === 'normal' ? 'Convertendo' : 'Verificar estado'}</strong></div>
          <ArrowRight aria-hidden="true" className="flow-arrow" />
        </li>
        <li>
          <UtilityPole aria-hidden="true" />
          <div><span>Rede CA</span><strong className="metric">{new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(snapshot.grid.voltageV)} V</strong></div>
        </li>
      </ol>
      <div className="pv-context">
        {!snapshot.pv2.active && <p>PV2 não utilizado</p>}
        {settings && <p>{settings.panelCount} × {settings.panelWattage} W · {capacity} instalados</p>}
      </div>
    </section>
  )
}
