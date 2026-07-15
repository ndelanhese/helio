import { expect, test } from '@playwright/test'
import { expectAccessible, preparePage, resetScenario, TEST_ADMIN, TEST_PASSWORD } from './support'

test('logout and login preserve a safe destination and work with the keyboard', async ({ page, request }) => {
  await resetScenario(request)
  await preparePage(page)
  await page.goto('/settings')

  await page.getByRole('button', { name: 'Sair do Helio' }).press('Enter')
  await expect(page).toHaveURL(/\/login/)
  await expect(page.getByRole('heading', { name: 'Entre no seu Helio' })).toBeVisible()
  await expectAccessible(page)
  await page.getByLabel('Usuário').fill(TEST_ADMIN)
  await page.getByLabel('Senha').fill(TEST_PASSWORD)
  await page.getByRole('button', { name: 'Entrar no Helio' }).press('Enter')

  await expect(page).toHaveURL(/\/settings$/)
  await expect(page.getByRole('heading', { name: 'O observatório, do seu jeito.' })).toBeVisible()
})
