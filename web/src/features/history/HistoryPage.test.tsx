import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it } from 'vitest'

import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { HistoryPage } from './HistoryPage'
import { buildHistoryView, getPeriodRange } from './history-model'
import { ProductionChart } from './ProductionChart'
import { SummaryCards } from './SummaryCards'

const current = {
  from: '2026-07-13T03:00:00.000Z', to: '2026-07-20T03:00:00.000Z', resolution: 'hour',
  points: [
    { at: '2026-07-14T12:00:00Z', energyWh: 1200, peakPowerW: 2800, productiveMinutes: 120, coveragePct: 92 },
    { at: '2026-07-14T13:00:00Z', energyWh: 1600, peakPowerW: 3400, productiveMinutes: 180, coveragePct: 96 },
    { at: '2026-07-14T16:00:00Z', energyWh: 900, peakPowerW: 2100, productiveMinutes: 60, coveragePct: 94 },
  ],
}
const previous = {
  from: '2026-07-06T03:00:00.000Z', to: '2026-07-13T03:00:00.000Z', resolution: 'hour',
  points: [
    { at: '2026-07-07T12:00:00Z', energyWh: 1400, peakPowerW: 3000, productiveMinutes: 150, coveragePct: 99 },
  ],
}
const settings = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.5, longitude: -46.6,
  loggerHost: '192.0.2.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

function useHistoryFixtures() {
  server.use(http.get('/api/v1/history', ({ request }) => {
    const params = new URL(request.url).searchParams
    return HttpResponse.json(params.get('from') === current.from ? current : previous)
  }))
}

describe('HistoryPage', () => {
  beforeEach(() => {
    window.history.replaceState({}, '', `/history?period=week&from=${encodeURIComponent(current.from)}&to=${encodeURIComponent(current.to)}`)
  })

  it('restores URL period state, compares the previous period and preserves CSV bounds', async () => {
    useHistoryFixtures()
    renderApp(<HistoryPage timezone="America/Sao_Paulo" />)

    expect(await screen.findByRole('heading', { name: 'Histórico solar' })).toBeVisible()
    expect(screen.getByRole('button', { name: 'Semana' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByText('3,70 kWh')).toBeVisible()
    expect(screen.getAllByText(/período anterior/i).length).toBeGreaterThan(0)
    expect(screen.getByText(/cobertura diferente/i)).toBeVisible()
    const exportLink = screen.getByRole('link', { name: /baixar csv/i })
    expect(exportLink).toHaveAttribute('href', `/api/v1/history.csv?from=${encodeURIComponent(current.from)}&to=${encodeURIComponent(current.to)}`)
  })

  it('shows an explicit dashed gap, low-coverage warning and precise accessible table', async () => {
    useHistoryFixtures()
    renderApp(<HistoryPage timezone="America/Sao_Paulo" />)

    const gapControl = await screen.findByRole('button', { name: /Sem dados entre 10:00 e 13:00/ })
    expect(gapControl.querySelector('svg')).toHaveAttribute('stroke-dasharray')
    await userEvent.hover(gapControl)
    expect(screen.getByRole('tooltip')).toHaveTextContent('Sem dados neste intervalo')
    expect(screen.getByRole('status')).toHaveTextContent(/1,7%.*abaixo de 95%/i)
    await userEvent.click(screen.getByText('Ver dados precisos'))
    const table = screen.getByRole('table', { name: 'Leituras do período atual' })
    expect(within(table).getByText('14/07/2026, 09:00')).toBeVisible()
    expect(within(table).getByText('2,80 kW')).toBeVisible()
  })

  it('updates URL query state and restores it on browser back navigation', async () => {
    useHistoryFixtures()
    renderApp(<HistoryPage timezone="America/Sao_Paulo" />)
    expect(await screen.findByText('3,70 kWh')).toBeVisible()

    await userEvent.click(screen.getByRole('button', { name: 'Mês' }))
    expect(new URLSearchParams(window.location.search).get('period')).toBe('month')

    window.history.replaceState({}, '', `/history?period=week&from=${encodeURIComponent(current.from)}&to=${encodeURIComponent(current.to)}`)
    fireEvent.popState(window)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Semana' })).toHaveAttribute('aria-pressed', 'true'))
  })

  it('explains an empty history without pretending production was zero', async () => {
    server.use(http.get('/api/v1/history', () => HttpResponse.json({ ...current, points: [] })))
    renderApp(<HistoryPage timezone="America/Sao_Paulo" />)
    expect(await screen.findByRole('heading', { name: 'Ainda não há leituras neste período.' })).toBeVisible()
    expect(screen.getByText(/não representa geração zero/i)).toBeVisible()
  })

  it('offers a retry after an API error', async () => {
    let attempts = 0
    server.use(http.get('/api/v1/history', () => {
      attempts += 1
      if (attempts <= 2) return HttpResponse.json({ error: { code: 'unavailable', message: 'offline' } }, { status: 503 })
      return HttpResponse.json(current)
    }))
    renderApp(<HistoryPage timezone="America/Sao_Paulo" />)
    expect(await screen.findByRole('heading', { name: 'Não foi possível carregar o histórico.' })).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Tentar novamente' }))
    expect(await screen.findByText('3,70 kWh')).toBeVisible()
  })

  it('shows an honest settings error and recovers the timezone on retry', async () => {
    let attempts = 0
    server.use(http.get('/api/v1/settings', () => {
      attempts += 1
      return attempts === 1
        ? HttpResponse.json({ error: { code: 'unavailable', message: 'offline' } }, { status: 503 })
        : HttpResponse.json(settings)
    }))
    useHistoryFixtures()
    renderApp(<HistoryPage />)
    expect(await screen.findByRole('heading', { name: 'Não foi possível carregar o fuso horário.' })).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Tentar carregar configurações' }))
    expect(await screen.findByRole('heading', { name: 'Histórico solar' })).toBeVisible()
  })

  it('keeps isolated readings out of the chart chrome', () => {
    const range = getPeriodRange('day', new Date('2026-07-14T15:00:00Z'), 'America/Sao_Paulo')
    const currentView = buildHistoryView([{ at: '2026-07-14T12:00:00Z', powerW: 1800 }], 'minute')
    const previousView = buildHistoryView([{ at: '2026-07-13T12:00:00Z', powerW: 1600 }], 'minute')
    renderApp(<ProductionChart current={currentView} previous={previousView} range={range} timezone="America/Sao_Paulo" />)
    expect(screen.queryByText(/Leitura isolada do período/i)).not.toBeInTheDocument()
  })

  it('qualifies even a sub-one-point coverage difference with neutral wording', () => {
    renderApp(<SummaryCards
      current={{ coveragePct: 94.4, energyWh: 1000, peakPowerW: 1000, productiveMinutes: 60 }}
      previous={{ coveragePct: 94, energyWh: 1000, peakPowerW: 1000, productiveMinutes: 60 }}
    />)
    expect(screen.getByText(/cobertura diferente.*compare com cautela/i)).toBeVisible()
  })

  it('renders 1,440 minute points without eagerly mounting precise table rows', async () => {
    const points = Array.from({ length: 1440 }, (_, minute) => ({
      at: new Date(Date.parse('2026-07-14T03:00:00Z') + minute * 60_000).toISOString(),
      powerW: Math.max(0, Math.sin((minute / 1440) * Math.PI) * 4200),
    }))
    server.use(http.get('/api/v1/history', () => HttpResponse.json({
      from: '2026-07-14T03:00:00.000Z', to: '2026-07-15T03:00:00.000Z', resolution: 'minute', points,
    })))
    window.history.replaceState({}, '', '/history?period=day&from=2026-07-14T03%3A00%3A00.000Z&to=2026-07-15T03%3A00%3A00.000Z')
    renderApp(<HistoryPage timezone="America/Sao_Paulo" />)
    expect(await screen.findByRole('heading', { name: 'Histórico solar' })).toBeVisible()
    expect(screen.queryAllByRole('row')).toHaveLength(0)
  })
})
