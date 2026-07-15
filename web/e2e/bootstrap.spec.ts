import { expect, test } from '@playwright/test'
import { expectAccessible, preparePage, resetScenario, TEST_ADMIN, TEST_LOGGER_HOST, TEST_LOGGER_SERIAL, TEST_PASSWORD } from './support'

test('first bootstrap completes the five-step local setup with the keyboard', async ({ page, request }) => {
  await resetScenario(request, 'bootstrap-open')
  await preparePage(page)
  await page.goto('/history')
  await expect(page).toHaveURL(/\/bootstrap$/)
  await expect(page.getByRole('heading', { name: 'Crie a conta local' })).toBeVisible()
  await expectAccessible(page)

  await page.getByLabel('Usuário administrador').fill(TEST_ADMIN)
  await page.getByLabel('Senha', { exact: true }).fill(TEST_PASSWORD)
  await page.getByLabel('Confirmar senha').fill(TEST_PASSWORD)
  await page.getByRole('button', { name: 'Continuar para o logger' }).press('Enter')
  await page.getByLabel('Endereço IP do logger').fill(TEST_LOGGER_HOST)
  await page.getByLabel('Número de série do logger').fill(TEST_LOGGER_SERIAL)
  await page.getByRole('button', { name: 'Continuar para os painéis' }).press('Enter')
  await expect(page.getByRole('heading', { name: 'Descreva os painéis' })).toBeVisible()
  await page.getByRole('button', { name: 'Continuar para local e tarifa' }).press('Enter')
  await page.getByRole('button', { name: 'Revisar configuração' }).press('Enter')
  await expect(page.getByRole('region', { name: 'Revisão da configuração' })).toBeVisible()
  await page.getByRole('button', { name: 'Criar Helio' }).press('Enter')

  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
})
