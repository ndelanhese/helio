import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, expect, it } from 'vitest'

import { server } from '../../test/server'
import { renderApp } from '../../test/render'
import { FinancePage } from './FinancePage'

afterEach(() => server.resetHandlers())

it('labels a no-meter projection as estimate', async () => {
  server.use(http.get('/api/v1/finance/summary', () => HttpResponse.json({ cycles: [], latestProjection: { id: 1, billingCycleId: 1, tariffVersionId: 1, consumptionMinor: 1, compensationMinor: 1, flagMinor: 1, taxesMinor: 1, cipMinor: 1, totalMinor: 14176, withoutSolarCompensationMinor: 33270, isEstimate: true, calculatedAt: '2026-07-14T00:00:00Z' } })), http.get('/api/v1/finance/tariff-proposals', () => HttpResponse.json({ proposals: [] })))
  renderApp(<FinancePage />)
  expect(await screen.findByText('Projeção estimada sem medidor')).toBeVisible()
})
