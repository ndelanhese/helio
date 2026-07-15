import { useQuery } from '@tanstack/react-query'

import { settingsQuery } from '../../api/queries'
import { HealthPanel } from './HealthPanel'
import { HeroPower } from './HeroPower'
import { MetricStrip } from './MetricStrip'
import { PVFlow } from './PVFlow'
import { useLiveTelemetry } from './useLiveTelemetry'
import { WeatherContext } from './WeatherContext'

export function NowPage() {
  const live = useLiveTelemetry()
  const settings = useQuery(settingsQuery)
  const snapshot = live.data?.snapshot

  if (!snapshot && live.isPending) return (
    <section aria-busy="true" aria-label="Carregando telemetria ao vivo" className="live-loading">
      <p>Buscando a leitura mais recente…</p>
      <div className="skeleton-power" /><div className="skeleton-line" />
    </section>
  )
  if (!snapshot) return (
    <section className="live-empty">
      <h1>A leitura do inversor ainda não chegou.</h1>
      <p>O Helio continuará tentando se reconectar sem substituir a ausência por zero.</p>
      <button className="secondary-action" type="button" onClick={() => { void live.refetch() }}>Buscar novamente</button>
    </section>
  )

  return (
    <article className="now-page">
      <HeroPower snapshot={snapshot} updatedAt={live.data?.lastSuccess ?? snapshot.observedAt} timezone={settings.data?.timezone} />
      <MetricStrip snapshot={snapshot} />
      <PVFlow snapshot={snapshot} settings={settings.data} />
      <div className="context-panels"><HealthPanel snapshot={snapshot} /><WeatherContext /></div>
    </article>
  )
}
