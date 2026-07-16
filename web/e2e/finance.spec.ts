import { test, expect } from './fixtures'
import { preparePage } from './support'

test('approves tariff and records a completed bill', async ({ page, setScenario }) => {
  await setScenario('finance')
  await preparePage(page)
  await page.goto('/finance')
  await page.getByRole('button', { name: 'Aprovar tarifa' }).click()
  await page.getByLabel('Leitura inicial').fill('2026-06-01')
  await page.getByLabel('Leitura final').fill('2026-07-01')
  await page.getByLabel('Consumo ativo (kWh)').fill('322')
  await page.getByLabel('Energia injetada (kWh)').fill('100')
  await page.getByLabel('Créditos usados (kWh)').fill('25')
  await page.getByLabel('Saldo de créditos (kWh)').fill('800')
  await page.getByLabel('Bandeira aplicada (centavos)').fill('1200')
  await page.getByLabel('Total pago (centavos)').fill('14176')
  await page.getByRole('button', { name: 'Salvar fatura' }).click()
  await expect(page.getByText('Real versus projetado')).toBeVisible()
})
