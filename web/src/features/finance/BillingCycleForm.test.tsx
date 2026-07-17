import { fireEvent, screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { renderApp } from '../../test/render'
import { BillingCycleForm } from './BillingCycleForm'

it('exposes the billing fields and reading endpoints', () => {
  renderApp(<BillingCycleForm disabled={false} onSave={async () => {}} />)
  expect(screen.getByLabelText('Leitura inicial')).toBeVisible()
  expect(screen.getByLabelText('Consumo ativo (kWh)')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Salvar fatura' })).toBeVisible()
})

it('submits reading dates as civil dates', async () => {
  let saved: unknown
  renderApp(<BillingCycleForm disabled={false} onSave={async (payload) => { saved = payload }} />)
  const fill = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } })
  fill('Leitura inicial', '2026-07-01')
  fill('Leitura final', '2026-07-31')
  fill('Consumo ativo (kWh)', '150')
  fill('Energia injetada (kWh)', '20')
  fill('Créditos usados (kWh)', '10')
  fill('Saldo de créditos (kWh)', '10')
  fill('Bandeira aplicada (centavos)', '50')
  fill('Total pago (centavos)', '12345')
  fireEvent.click(screen.getByRole('button', { name: 'Salvar fatura' }))
  expect(saved).toMatchObject({ readingStart: '2026-07-01', readingEnd: '2026-07-31' })
})
