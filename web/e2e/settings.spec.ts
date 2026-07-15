import { expect, test } from '@playwright/test'
import { attachScreenshot, expectAccessible, expectNoHorizontalOverflow, preparePage, resetScenario } from './support'

test('settings validate, edit, save, back up, and show low-confidence insights', async ({ page, request }, testInfo) => {
  await resetScenario(request)
  await preparePage(page)
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'O observatório, do seu jeito.' })).toBeVisible()
  await expectAccessible(page)

  await page.getByLabel('Quantidade de painéis').fill('0')
  await page.getByRole('button', { name: 'Salvar configurações' }).press('Enter')
  await expect(page.getByLabel('Quantidade de painéis')).toHaveAttribute('aria-invalid', 'true')
  await page.getByLabel('Quantidade de painéis').fill('8')
  await page.getByRole('button', { name: 'Salvar configurações' }).press('Enter')
  await expect(page.getByRole('status', { name: 'Configurações salvas.' })).toBeVisible()

  const backup = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Baixar backup consistente' }).click()
  await expect((await backup).suggestedFilename()).toBe('helio-backup-20260714-154300.db')
  if (testInfo.project.name === 'desktop-chromium') await attachScreenshot(page, testInfo, 'settings')

  await page.getByRole('link', { name: 'Insights' }).press('Enter')
  await expect(page.getByRole('heading', { name: 'Telemetria insuficiente para comparar' })).toBeVisible()
  await expect(page.getByRole('article')).toContainText('Confiança baixa')
  await expectNoHorizontalOverflow(page)
  await expectAccessible(page)
})
