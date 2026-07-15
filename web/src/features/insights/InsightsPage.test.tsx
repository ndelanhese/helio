import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { describe, expect, it } from 'vitest'

import { renderApp } from '../../test/render'
import { server } from '../../test/server'
import { InsightsPage } from './InsightsPage'

const settings = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.5, longitude: -46.6,
  loggerHost: '192.168.1.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

describe('InsightsPage', () => {
  it('explains low confidence, evidence, observation window, active alerts and recovery', async () => {
    server.use(
      http.get('/api/v1/settings', () => HttpResponse.json(settings)),
      http.get('/api/v1/insights', () => HttpResponse.json({
        version: 'v1', day: '2026-07-14', actualWh: 7_200, expectedWh: 10_000, ratio: 0.72,
        confidence: 'low', qualifying: false,
        evidence: [{ code: 'history_days', label: 'Histórico qualificável', value: 4, unit: 'days' }],
        observationWindow: { qualifyingDays: 4, minimumDays: 7 },
        trends: {
          peakPower: { direction: 'insufficient', changePct: 0, windowDays: 4 },
          productiveMinutes: { direction: 'insufficient', changePct: 0, windowDays: 4 },
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
    expect(screen.getByRole('heading', { name: 'Alertas ativos' })).toBeVisible()
    expect(screen.getByText('Produção abaixo da referência')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Recuperações recentes' })).toBeVisible()
    expect(screen.getByText('A leitura voltou a ficar atualizada.')).toBeVisible()
    const unsupportedClaims = new RegExp(['con', 'sumo|auto', 'consumo|import', 'ação|export', 'ação|econo', 'mia|poup', 'ança'].join(''), 'i')
    expect(document.body.textContent).not.toMatch(unsupportedClaims)
  })
})
