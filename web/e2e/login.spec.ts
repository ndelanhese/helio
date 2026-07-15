import { test, expect } from './fixtures'
import { expectAccessible, preparePage, startKeyboard, tabUntil, TEST_ADMIN, TEST_PASSWORD } from './support'

test('logout and login preserve a safe destination by keyboard only', async ({ page }, testInfo) => {
  await preparePage(page)
  await page.goto('/settings')
  await startKeyboard(page)
  await tabUntil(page, page.getByRole('button', { name: 'Sair do Helio' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/login/)
  await expect(page.getByRole('heading', { name: 'Entre no seu Helio' })).toBeVisible()
  await expectAccessible(page)

  await startKeyboard(page)
  await tabUntil(page, page.getByLabel('Usuário'), testInfo.project.name)
  await page.keyboard.type(TEST_ADMIN)
  await tabUntil(page, page.getByLabel('Senha'), testInfo.project.name)
  await page.keyboard.type(TEST_PASSWORD)
  await tabUntil(page, page.getByRole('button', { name: 'Entrar no Helio' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(page).toHaveURL(/\/settings$/)
})
