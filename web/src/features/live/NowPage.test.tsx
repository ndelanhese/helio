import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { NowPage } from './NowPage'

const capturedAt = '2026-07-14T15:42:00Z'
const liveState = {
  lastSuccess: capturedAt,
  stale: false,
  snapshot: {
    observedAt: capturedAt,
    status: 'normal',
    acPowerW: 2070,
    energyTodayWh: 12340,
    energyLifetimeWh: 4567800,
    pv1: { active: true, voltageV: 267.1, currentA: 8, powerW: 2070 },
    pv2: { active: false, voltageV: 0, currentA: 0, powerW: 0 },
    grid: { voltageV: 267.1, frequencyHz: 59.97 },
    faultCodes: [] as number[],
  },
}

const settings = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.5, longitude: -46.6,
  loggerHost: '192.168.1.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

class QuietEventSource {
  static instances: QuietEventSource[] = []
  onerror: ((event: Event) => void) | null = null
  onopen: ((event: Event) => void) | null = null
  listeners = new Map<string, (event: MessageEvent) => void>()
  close = vi.fn()

  constructor(public readonly url: string) { QuietEventSource.instances.push(this) }
  addEventListener(type: string, listener: EventListener) {
    this.listeners.set(type, listener as (event: MessageEvent) => void)
  }
  removeEventListener(type: string) { this.listeners.delete(type) }
}

function useFixture(state = liveState) {
  server.use(
    http.get('/api/v1/live', () => HttpResponse.json(state)),
    http.get('/api/v1/settings', () => HttpResponse.json(settings)),
  )
}

describe('NowPage', () => {
  beforeEach(() => {
    vi.setSystemTime(new Date('2026-07-14T15:42:10Z'))
    QuietEventSource.instances = []
    vi.stubGlobal('EventSource', QuietEventSource)
  })

  it('presents the latest real inverter snapshot in Brazilian Portuguese', async () => {
    useFixture()
    renderApp(<NowPage />)

    expect(await screen.findByRole('heading', { name: '2,07 kW' })).toBeVisible()
    expect(screen.getByText('12,34 kWh')).toBeVisible()
    expect(screen.getByText('4.567,80 kWh')).toBeVisible()
    expect(screen.getAllByText('267,1 V').length).toBeGreaterThan(0)
    expect(screen.getByText('8,00 A')).toBeVisible()
    expect(screen.getByText('59,97 Hz')).toBeVisible()
    expect(screen.getByText('Operação normal')).toBeVisible()
    expect(screen.getByText(/Atualizado às 12:42/)).toBeVisible()
    expect(screen.getByText('PV1')).toBeVisible()
    expect(screen.getByText('PV2 não utilizado')).toBeVisible()
    expect(screen.queryByText(/consumo|exportação/i)).not.toBeInTheDocument()
    expect(screen.getByText('Previsão indisponível')).toBeVisible()
  })

  it('keeps the last measurement visible when it becomes stale', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    useFixture()
    renderApp(<NowPage />)
    expect(await screen.findByRole('heading', { name: '2,07 kW' })).toBeVisible()

    await vi.advanceTimersByTimeAsync(21_000)

    expect(screen.getByText('Dados desatualizados')).toBeVisible()
    expect(screen.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: '0 W' })).not.toBeInTheDocument()
    vi.useRealTimers()
  })

  it('shows fault severity and codes without replacing production metrics', async () => {
    useFixture({
      ...liveState,
      snapshot: { ...liveState.snapshot, status: 'fault', faultCodes: [1, 3, 55] },
    })
    renderApp(<NowPage />)

    expect(await screen.findByRole('heading', { name: '2,07 kW' })).toBeVisible()
    expect(screen.getByText('Falha crítica')).toBeVisible()
    expect(screen.getByText(/Códigos 1, 3 e 55/)).toBeVisible()
    expect(screen.queryByText(/PV2.*falha/i)).not.toBeInTheDocument()
  })

  it('uses a meaningful loading skeleton before the first snapshot', () => {
    server.use(
      http.get('/api/v1/live', async () => new Promise(() => undefined)),
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
    )
    renderApp(<NowPage />)
    expect(screen.getByText('Buscando a leitura mais recente…')).toBeVisible()
    expect(screen.getByLabelText('Carregando telemetria ao vivo')).toBeVisible()
  })

  it('shows PV2 as a measured channel when it is configured active', async () => {
    server.use(
      http.get('/api/v1/live', () => HttpResponse.json({
        ...liveState,
        snapshot: { ...liveState.snapshot, pv2: { active: true, voltageV: 180, currentA: 4, powerW: 720 } },
      })),
      http.get('/api/v1/settings', () => HttpResponse.json({ ...settings, activeMPPT: [1, 2] })),
    )
    renderApp(<NowPage />)

    expect(await screen.findByText('PV2')).toBeVisible()
    expect(screen.getByText('720 W')).toBeVisible()
    expect(screen.queryByText('PV2 não utilizado')).not.toBeInTheDocument()
  })
})
