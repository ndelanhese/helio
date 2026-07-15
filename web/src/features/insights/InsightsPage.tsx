import { useQuery } from '@tanstack/react-query'

import { alertsQuery, insightsQuery, settingsQuery } from '../../api/queries'
import { AlertList } from './AlertList'
import { InsightCard } from './InsightCard'

function previousLocalDay(timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', { day: '2-digit', month: '2-digit', timeZone: timezone, year: 'numeric' }).formatToParts(new Date())
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  const localNoon = new Date(`${values.year}-${values.month}-${values.day}T12:00:00Z`)
  localNoon.setUTCDate(localNoon.getUTCDate() - 1)
  return localNoon.toISOString().slice(0, 10)
}

export function InsightsPage() {
  const settings = useQuery(settingsQuery)
  const day = settings.data?.timezone ? previousLocalDay(settings.data.timezone) : ''
  const insight = useQuery(insightsQuery(day))
  const active = useQuery(alertsQuery('open'))
  const resolved = useQuery(alertsQuery('resolved'))

  if (settings.isPending || insight.isPending || active.isPending || resolved.isPending) return <section className="insights-state" aria-busy="true"><p>Organizando evidências da geração…</p></section>
  if (!insight.data || !active.data || !resolved.data) return <section className="insights-state"><h1>A análise não está disponível.</h1><p>A geração ao vivo e o histórico continuam disponíveis enquanto o Helio tenta novamente.</p><button className="secondary-action" type="button" onClick={() => { void insight.refetch(); void active.refetch(); void resolved.refetch() }}>Tentar novamente</button></section>

  return (
    <article className="insights-page">
      <header className="insights-heading"><p className="eyebrow">Leitura explicável</p><h1>O que os seus dados mostram.</h1><p>Análise de {new Intl.DateTimeFormat('pt-BR', { dateStyle: 'long', timeZone: 'UTC' }).format(new Date(`${insight.data.day}T12:00:00Z`))}.</p></header>
      <InsightCard insight={insight.data} />
      <section className="generated-value" aria-label="Valor estimado"><span>{insight.data.generatedEnergyValue.label}</span><strong>{new Intl.NumberFormat('pt-BR', { style: 'currency', currency: insight.data.generatedEnergyValue.currency }).format(insight.data.generatedEnergyValue.minor / 100)}</strong><p>Estimativa baseada somente na energia gerada medida e na tarifa configurada.</p></section>
      <div className="alerts-layout"><AlertList alerts={active.data.alerts} state="open" /><AlertList alerts={resolved.data.alerts} state="resolved" /></div>
    </article>
  )
}
