import { screen } from '@testing-library/react'
import { expect, it } from 'vitest'
import { renderApp } from '../../test/render'
import { BillingCycleForm } from './BillingCycleForm'

it('exposes the billing fields and reading endpoints', () => {
  renderApp(<BillingCycleForm disabled={false} onSave={async () => {}} />)
  expect(screen.getByLabelText('Leitura inicial')).toBeVisible()
  expect(screen.getByLabelText('Consumo ativo (kWh)')).toBeVisible()
  expect(screen.getByRole('button', { name: 'Salvar fatura' })).toBeVisible()
})
