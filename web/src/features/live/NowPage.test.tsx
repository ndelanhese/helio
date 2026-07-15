import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { AppShell } from '../../components/layout/AppShell'
import { NowPage } from './NowPage'
import { useLiveStatus } from './useLiveTelemetry'

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
  loggerHost: '192.0.2.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
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

function LiveHarness() {
  const status = useLiveStatus()
  return <AppShell announcement={status.announcement} connectionState={status.connectionState} currentPath="/"><NowPage /></AppShell>
}

describe('NowPage', () => {
  beforeEach(() => {
    vi.setSystemTime(new Date('2026-07-14T15:42:10Z'))
    QuietEventSource.instances = []
    vi.stubGlobal('EventSource', QuietEventSource)
  })

  it('presents the latest real inverter snapshot in Brazilian Portuguese', async () => {
    useFixture()
    renderApp(<LiveHarness />)

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
    const unsupportedClaim = new RegExp(['con', 'sumo|export', 'ação'].join(''), 'i')
    expect(screen.queryByText(unsupportedClaim)).not.toBeInTheDocument()
    expect(screen.getByText('Previsão indisponível')).toBeVisible()
  })

  it('shows stale weather age and reduced confidence without affecting live telemetry', async () => {
	useFixture()
	server.use(http.get('/health/components', () => HttpResponse.json({
		database: 'ok', logger: 'online', collector: 'running', weather: 'stale',
		weatherFetchedAt: '2026-07-14T13:42:10Z', weatherUpdatedAt: '2026-07-14T15:42:10Z',
	})))
	renderApp(<NowPage />)
	expect(await screen.findByRole('heading', { name: '2,07 kW' })).toBeVisible()
	expect(await screen.findByRole('heading', { name: 'Dados meteorológicos desatualizados' })).toBeVisible()
	expect(screen.getByText(/Atualizados há 2 horas/)).toBeVisible()
	expect(screen.getByText('Confiança meteorológica reduzida')).toBeVisible()
  })

	it('shows current weather conditions with live solar radiation', async () => {
		useFixture()
		server.use(http.get('/health/components', () => HttpResponse.json({
			database: 'ok', logger: 'online', collector: 'running', weather: 'available',
			temperatureC: 22.4, precipitationMM: 0.3, weatherCode: 61, cloudCoverPct: 78, windSpeedKMH: 14.2, irradianceWM2: 645.8, weatherFetchedAt: '2026-07-14T13:40:00Z', weatherUpdatedAt: '2026-07-14T15:40:00Z',
		})))
		renderApp(<NowPage />)
		expect(await screen.findByRole('heading', { name: '22,4 °C' })).toBeVisible()
		expect(screen.getByText('Chuva')).toBeVisible()
		expect(screen.getByText('0,3 mm')).toBeVisible()
		expect(screen.getByText('Vento')).toBeVisible()
		expect(screen.getByText('14,2 km/h')).toBeVisible()
		expect(screen.getByText('Nuvens')).toBeVisible()
		expect(screen.getByText('78%')).toBeVisible()
		expect(screen.getByText('Radiação solar')).toBeVisible()
		expect(screen.getByText('646 W/m²')).toBeVisible()
		expect(screen.getByText('Potencial solar')).toBeVisible()
		expect(screen.getByText('2,76 kW')).toBeVisible()
		expect(screen.getByText('75% do potencial')).toBeVisible()
		const modelInfo = screen.getByRole('button', { name: 'Como este potencial é estimado' })
		expect(modelInfo).toBeVisible()
		await userEvent.hover(modelInfo)
		expect(screen.getByRole('tooltip')).toHaveTextContent('Radiação modelada para sua localização')
		expect(screen.getByText('Chuva moderada')).toBeVisible()
		expect(screen.getByText('Atualizados há 2 minutos.')).toBeVisible()
	})

  it('keeps the last measurement visible when it becomes stale', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    vi.setSystemTime(new Date('2026-07-14T15:42:10Z'))
    useFixture()
    renderApp(<LiveHarness />)
    expect(await screen.findByRole('heading', { name: '2,07 kW' })).toBeVisible()
    QuietEventSource.instances.at(-1)?.onopen?.(new Event('open'))

    await vi.advanceTimersByTimeAsync(31_000)

    expect(screen.getByText(/Dados desatualizados/, { selector: '.connection-badge' })).toBeVisible()
    expect(screen.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
    expect(screen.queryByRole('heading', { name: '0 W' })).not.toBeInTheDocument()
    vi.useRealTimers()
  })

  it('shows a neutral fault label and codes without replacing production metrics', async () => {
    useFixture({
      ...liveState,
      snapshot: { ...liveState.snapshot, status: 'fault', faultCodes: [1, 3, 55] },
    })
    renderApp(<NowPage />)

    expect(await screen.findByRole('heading', { name: '2,07 kW' })).toBeVisible()
    expect(screen.getByText('Falha informada pelo inversor')).toBeVisible()
    expect(screen.getByText(/Códigos 1, 3 e 55/)).toBeVisible()
    expect(screen.queryByText(/PV2.*falha/i)).not.toBeInTheDocument()
  })

  it('handles a code-less inverter fault without invented severity or undefined text', async () => {
    useFixture({ ...liveState, snapshot: { ...liveState.snapshot, status: 'fault', faultCodes: [] } })
    renderApp(<NowPage />)
    expect(await screen.findByText('Falha informada pelo inversor')).toBeVisible()
    expect(screen.getByText('O inversor não informou códigos para esta falha.')).toBeVisible()
    expect(screen.queryByText(/crítica|undefined/i)).not.toBeInTheDocument()
  })

  it('shows an honest unavailable state when the live query fails', async () => {
    server.use(
      http.get('/api/v1/live', () => HttpResponse.json({ error: { code: 'unavailable', message: 'offline' } }, { status: 503 })),
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
    )
    renderApp(<NowPage />)
    expect(await screen.findByText('A leitura do inversor ainda não chegou.')).toBeVisible()
    expect(screen.getByText(/sem substituir a ausência por zero/i)).toBeVisible()
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
