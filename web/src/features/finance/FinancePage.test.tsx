import { screen } from '@testing-library/react'
import { http, HttpResponse } from 'msw'
import { afterEach, expect, it } from 'vitest'

import { server } from '../../test/server'
import { renderApp } from '../../test/render'
import { FinancePage } from './FinancePage'

afterEach(() => server.resetHandlers())

it('labels a no-meter projection as estimate and displays separate flag amounts', async () => {
  server.use(http.get('/api/v1/finance/summary', () => HttpResponse.json({ cycles: [], creditBalanceKWh: 0, nextCreditExpiry: null, latestProjection: { id: 1, billingCycleId: 1, tariffVersionId: 1, consumptionMinor: 1, compensationMinor: 1, flagMinor: 1, flagChargeMinor: 50, taxesMinor: 1, cipMinor: 1, totalMinor: 14176, withoutSolarCompensationMinor: 33270, isEstimate: true, calculatedAt: '2026-07-14T00:00:00Z', displayTotal: 'R$ 141,76', displayWithoutSolar: 'R$ 332,70', displayRows: [{ label: 'Bandeira tarifária', value: 'R$ 0,01' }, { label: 'Ajuste manual de bandeira', value: 'R$ 0,50' }] } })), http.get('/api/v1/finance/tariff-proposals', () => HttpResponse.json({ proposals: [] })))
  renderApp(<FinancePage />)
  expect(await screen.findByText('Projeção estimada sem medidor')).toBeVisible()
  expect(screen.getByText('Bandeira tarifária')).toBeVisible()
  expect(screen.getByText('Ajuste manual de bandeira')).toBeVisible()
})
