import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { InsightsPage } from './InsightsPage'
import { InsightCard } from './InsightCard'

const settings = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.5, longitude: -46.6,
  loggerHost: '192.168.1.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

describe('InsightsPage', () => {
  it.each([
    ['low', 'Confiança baixa'],
    ['medium', 'Confiança média'],
    ['high', 'Confiança alta'],
  ] as const)('labels %s confidence without overstating the conclusion', (confidence, label) => {
    renderApp(<InsightCard insight={{
      version: 'v1', day: '2026-07-14', actualWh: 7_200, expectedWh: 10_000, ratio: 0.72,
      confidence, qualifying: true, evidence: [], observationWindow: { qualifyingDays: 10, minimumDays: 7 },
      trends: {
        peakPower: { direction: 'stable', current: 2_600, previous: 2_600, delta: 0, deltaPct: 0, coveragePct: 100, windowDays: 7 },
        productiveMinutes: { direction: 'stable', current: 300, previous: 300, delta: 0, deltaPct: 0, coveragePct: 100, windowDays: 7 },
      },
      generatedEnergyValue: { minor: 684, currency: 'BRL', label: 'valor estimado da energia gerada', estimate: true },
    }} />)
    expect(screen.getByText(label)).toBeVisible()
  })
  it('explains low confidence, evidence, observation window, active alerts and recovery', async () => {
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
      http.get('/api/v1/insights', () => HttpResponse.json({
        version: 'v1', day: '2026-07-14', actualWh: 7_200, expectedWh: 10_000, ratio: 0.72,
        confidence: 'low', qualifying: false,
        evidence: [{ code: 'history_days', label: 'Histórico qualificável', value: 4, unit: 'days' }],
        observationWindow: { qualifyingDays: 4, minimumDays: 7 },
        trends: {
          peakPower: { direction: 'up', current: 2600, previous: 2000, delta: 600, deltaPct: 30, coveragePct: 95, windowDays: 7 },
          productiveMinutes: { direction: 'insufficient', current: 0, previous: 0, delta: 0, deltaPct: 0, coveragePct: 72, windowDays: 4 },
        },
        generatedEnergyValue: { minor: 684, currency: 'BRL', label: 'valor estimado da energia gerada', estimate: true },
      })),
      http.get('/api/v1/alerts', ({ request }) => {
        const state = new URL(request.url).searchParams.get('state')
        return HttpResponse.json({ version: 'v1', state, alerts: state === 'open' ? [{
          kind: 'persistent_underproduction', state: 'open', severity: 'warning',
          title: 'Produção abaixo da referência', summary: 'Três dias qualificáveis abaixo da expectativa.',
          openedAt: '2026-07-14T03:05:00Z', resolvedAt: null,
          evidence: [{ label: 'Relação real/esperada', value: 0.62, unit: 'ratio' }],
        }] : [{
          kind: 'telemetry_stale', state: 'resolved', severity: 'warning',
          title: 'Telemetria desatualizada', summary: 'A leitura voltou a ficar atualizada.',
          openedAt: '2026-07-12T12:00:00Z', resolvedAt: '2026-07-12T12:04:00Z', evidence: [],
        }] })
      }),
    )
    renderApp(<InsightsPage />)

    expect(await screen.findByRole('heading', { name: 'Histórico ainda insuficiente' })).toBeVisible()
    expect(screen.getByText('Confiança baixa')).toBeVisible()
    expect(screen.getByText(/4 de 7 dias qualificáveis/)).toBeVisible()
    expect(screen.getByText(/Histórico qualificável/)).toBeVisible()
    expect(screen.getByText('valor estimado da energia gerada')).toBeVisible()
    expect(screen.getByText(/R\$\s*6,84/)).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Potência de pico' })).toBeVisible()
    expect(screen.getByText(/30% acima/)).toBeVisible()
    expect(screen.getByText(/72% de cobertura/)).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Alertas ativos' })).toBeVisible()
    expect(screen.getByText('Produção abaixo da referência')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Recuperações recentes' })).toBeVisible()
    expect(screen.getByText('A leitura voltou a ficar atualizada.')).toBeVisible()
    const unsupportedClaims = new RegExp(['con', 'sumo|auto', 'consumo|import', 'ação|export', 'ação|econo', 'mia|poup', 'ança'].join(''), 'i')
    expect(document.body.textContent).not.toMatch(unsupportedClaims)
  })

  it('shows a settings-specific retry instead of waiting forever', async () => {
    let requests = 0
    server.use(http.get('/api/v1/settings', () => {
      requests += 1
      return HttpResponse.json({ error: { code: 'internal_error', message: 'failure' } }, { status: 500 })
    }))
    renderApp(<InsightsPage />)

    expect(await screen.findByRole('heading', { name: 'Não foi possível carregar as configurações.' })).toBeVisible()
    await userEvent.click(screen.getByRole('button', { name: 'Tentar novamente' }))
    expect(requests).toBeGreaterThan(1)
  })

  it('treats a missing daily insight as insufficient onboarding data', async () => {
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
      http.get('/api/v1/insights', () => HttpResponse.json({ error: { code: 'insights_not_found', message: 'not found' } }, { status: 404 })),
      http.get('/api/v1/alerts', ({ request }) => HttpResponse.json({ version: 'v1', state: new URL(request.url).searchParams.get('state'), limit: 100, alerts: [] })),
    )
    renderApp(<InsightsPage />)

    expect(await screen.findByRole('heading', { name: 'Ainda não há análise para este dia.' })).toBeVisible()
    expect(screen.getByText(/histórico diário suficiente/)).toBeVisible()
    expect(screen.queryByRole('heading', { name: 'A análise não está disponível.' })).not.toBeInTheDocument()
  })
})
