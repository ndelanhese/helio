import { useQueries, useQuery } from '@tanstack/react-query'
import { Download, RotateCw } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { api } from '../../api/client'
import { settingsQuery } from '../../api/queries'
import type { HistoryResponse } from '../../api/types'
import { AccessibleHistoryTable } from './AccessibleHistoryTable'
import {
  buildHistoryView,
  getPeriodRange,
  parseHistorySearch,
  serializeHistoryRange,
  type HistoryPeriod,
  type HistoryPoint,
  type HistoryRange,
} from './history-model'
import { PeriodPicker } from './PeriodPicker'
import { ProductionChart } from './ProductionChart'
import { SummaryCards } from './SummaryCards'

function historyPath(from: string, to: string, resolution: string) {
  const query = new URLSearchParams({ from, to, resolution })
  return `/history?${query.toString()}`
}

function exportPath(range: HistoryRange) {
  return `/api/v1/history.csv?from=${encodeURIComponent(range.from)}&to=${encodeURIComponent(range.to)}`
}

function requestOptions(range: HistoryRange, previous = false) {
  const from = previous ? range.previousFrom : range.from
  const to = previous ? range.previousTo : range.to
  return {
    queryKey: ['history', range.period, previous ? 'previous' : 'current', from, to, range.resolution] as const,
    queryFn: ({ signal }: { signal: AbortSignal }) => api.request<HistoryResponse>(historyPath(from, to, range.resolution), { signal }),
  }
}

function rangeTitle(range: HistoryRange, timezone: string) {
  const formatter = new Intl.DateTimeFormat('pt-BR', { day: '2-digit', month: 'long', timeZone: timezone, year: 'numeric' })
  const endInclusive = new Date(Date.parse(range.to) - 1)
  return `${formatter.format(new Date(range.from))} — ${formatter.format(endInclusive)}`
}

export function HistoryPage({ timezone: timezoneOverride }: { timezone?: string }) {
  const settings = useQuery({ ...settingsQuery, enabled: timezoneOverride === undefined })
  const timezone = timezoneOverride ?? settings.data?.timezone
  if (!timezone) return <section aria-busy="true" className="history-loading">Preparando o calendário local…</section>
  return <HistoryContent timezone={timezone} />
}

function HistoryContent({ timezone }: { timezone: string }) {
  const [range, setRange] = useState(() => parseHistorySearch(new URLSearchParams(window.location.search), new Date(), timezone))
  const queries = useQueries({ queries: [requestOptions(range), requestOptions(range, true)] })
  const [currentQuery, previousQuery] = queries

  useEffect(() => {
    const onPopState = () => setRange(parseHistorySearch(new URLSearchParams(window.location.search), new Date(), timezone))
    window.addEventListener('popstate', onPopState)
    return () => window.removeEventListener('popstate', onPopState)
  }, [timezone])

  useEffect(() => {
    const next = serializeHistoryRange(range).toString()
    if (window.location.search.slice(1) !== next) window.history.replaceState(window.history.state, '', `/history?${next}`)
  }, [range])

  const current = useMemo(() => currentQuery.data ? buildHistoryView(currentQuery.data.points as HistoryPoint[], range.resolution) : undefined, [currentQuery.data, range.resolution])
  const previous = useMemo(() => previousQuery.data ? buildHistoryView(previousQuery.data.points as HistoryPoint[], range.resolution) : undefined, [previousQuery.data, range.resolution])

  const changePeriod = (period: HistoryPeriod) => {
    const next = getPeriodRange(period, new Date(), timezone)
    window.history.pushState({}, '', `/history?${serializeHistoryRange(next).toString()}`)
    setRange(next)
  }

  if (currentQuery.isPending || previousQuery.isPending) return (
    <section aria-busy="true" aria-label="Carregando histórico" className="history-loading">
      <p>Organizando as leituras do período…</p><div className="history-skeleton" />
    </section>
  )
  if (currentQuery.isError || previousQuery.isError) return (
    <section className="history-state">
      <p className="eyebrow">Histórico indisponível</p>
      <h1>Não foi possível carregar o histórico.</h1>
      <p>O Helio não recebeu as leituras deste período. Verifique a conexão e tente novamente.</p>
      <button className="secondary-action" onClick={() => { void currentQuery.refetch(); void previousQuery.refetch() }} type="button"><RotateCw aria-hidden="true" />Tentar novamente</button>
    </section>
  )
  if (!current || current.chartPoints.length === 0) return (
    <section className="history-state">
      <p className="eyebrow">Período sem observações</p>
      <h1>Ainda não há leituras neste período.</h1>
      <p>A ausência de amostras não representa geração zero. Escolha outro período ou aguarde novas coletas.</p>
      <PeriodPicker onChange={changePeriod} value={range.period} />
    </section>
  )

  return (
    <article className="history-page">
      <header className="history-heading">
        <div><p className="eyebrow">Arquivo de produção</p><h1>Histórico solar</h1><p>{rangeTitle(range, timezone)}</p></div>
        <a className="export-link" download href={exportPath(range)}><Download aria-hidden="true" />Baixar CSV</a>
      </header>
      <PeriodPicker onChange={changePeriod} value={range.period} />
      <SummaryCards current={current.summary} previous={previous?.summary} />
      {current.hasLowCoverage && current.summary.coveragePct !== null && (
        <p className="coverage-warning" role="status">Cobertura de {Math.round(current.summary.coveragePct)}%, abaixo de 95%. Totais e comparação podem estar incompletos.</p>
      )}
      <ProductionChart current={current} previous={previous} range={range} timezone={timezone} />
      <AccessibleHistoryTable points={currentQuery.data.points as HistoryPoint[]} timezone={timezone} />
    </article>
  )
}
