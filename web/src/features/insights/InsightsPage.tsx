import { useQuery } from '@tanstack/react-query'

import { ApiError } from '../../api/client'
import { alertsQuery, insightsQuery, settingsQuery } from '../../api/queries'
import type { TrendDTO } from '../../api/types'
import { AlertList } from './AlertList'
import { InsightCard } from './InsightCard'

function previousLocalDay(timezone: string) {
  const parts = new Intl.DateTimeFormat('en-CA', { day: '2-digit', month: '2-digit', timeZone: timezone, year: 'numeric' }).formatToParts(new Date())
  const values = Object.fromEntries(parts.map((part) => [part.type, part.value]))
  const localNoon = new Date(`${values.year}-${values.month}-${values.day}T12:00:00Z`)
  localNoon.setUTCDate(localNoon.getUTCDate() - 1)
  return localNoon.toISOString().slice(0, 10)
}

function Trend({ label, trend, unit }: { label: string; trend: TrendDTO; unit: string }) {
  const number = new Intl.NumberFormat('pt-BR', { maximumFractionDigits: 1 })
  const direction = trend.direction === 'up' ? 'acima' : trend.direction === 'down' ? 'abaixo' : 'estável'
  return <section className="trend-card"><h3>{label}</h3>{trend.direction === 'insufficient'
    ? <p>Dados insuficientes · {trend.windowDays} dias · {number.format(trend.coveragePct)}% de cobertura.</p>
    : <><strong>{number.format(Math.abs(trend.deltaPct))}% {direction}</strong><p>Atual: {number.format(trend.current)} {unit} · anterior: {number.format(trend.previous)} {unit} · {number.format(trend.coveragePct)}% de cobertura.</p></>}</section>
}

export function InsightsPage() {
  const settings = useQuery(settingsQuery)
  const day = settings.data?.timezone ? previousLocalDay(settings.data.timezone) : ''
  const insight = useQuery(insightsQuery(day))
  const active = useQuery(alertsQuery('open', Boolean(settings.data)))
  const resolved = useQuery(alertsQuery('resolved', Boolean(settings.data)))

  if (settings.isPending) return <section className="insights-state" aria-busy="true"><p>Carregando as configurações da análise…</p></section>
  if (settings.isError) return <section className="insights-state"><h1>Não foi possível carregar as configurações.</h1><p>O Helio precisa do fuso horário e da tarifa configurada para preparar esta leitura.</p><button className="secondary-action" type="button" onClick={() => { void settings.refetch() }}>Tentar novamente</button></section>
  if (insight.isPending || active.isPending || resolved.isPending) return <section className="insights-state" aria-busy="true"><p>Organizando evidências da geração…</p></section>
  if (insight.error instanceof ApiError && insight.error.status === 404 && insight.error.code === 'insights_not_found') return <section className="insights-state"><h1>Ainda não há análise para este dia.</h1><p>O Helio está reunindo histórico diário suficiente para formar uma referência honesta.</p><button className="secondary-action" type="button" onClick={() => { void insight.refetch() }}>Verificar novamente</button></section>
  if (!insight.data || !active.data || !resolved.data) return <section className="insights-state"><h1>A análise não está disponível.</h1><p>A geração ao vivo e o histórico continuam disponíveis enquanto o Helio tenta novamente.</p><button className="secondary-action" type="button" onClick={() => { void insight.refetch(); void active.refetch(); void resolved.refetch() }}>Tentar novamente</button></section>

  return (
    <article className="insights-page">
      <header className="insights-heading"><p className="eyebrow">Leitura explicável</p><h1>O que os seus dados mostram.</h1><p>Análise de {new Intl.DateTimeFormat('pt-BR', { dateStyle: 'long', timeZone: 'UTC' }).format(new Date(`${insight.data.day}T12:00:00Z`))}.</p></header>
      <InsightCard insight={insight.data} />
      <section className="trends-section" aria-labelledby="trends-title"><div><p className="eyebrow">Comparação recente</p><h2 id="trends-title">Tendências com contexto.</h2></div><div className="trend-grid"><Trend label="Potência de pico" trend={insight.data.trends.peakPower} unit="W" /><Trend label="Minutos produtivos" trend={insight.data.trends.productiveMinutes} unit="min" /></div></section>
      <section className="generated-value" aria-label="Valor estimado"><span>{insight.data.generatedEnergyValue.label}</span><strong>{new Intl.NumberFormat('pt-BR', { style: 'currency', currency: insight.data.generatedEnergyValue.currency }).format(insight.data.generatedEnergyValue.minor / 100)}</strong><p>Estimativa baseada somente na energia gerada medida e na tarifa configurada.</p></section>
      <div className="alerts-layout"><AlertList alerts={active.data.alerts} state="open" /><AlertList alerts={resolved.data.alerts} state="resolved" /></div>
    </article>
  )
}
