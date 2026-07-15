import { test, expect } from './fixtures'
import { isScreenshotProject, preparePage, screenshotOptions, startKeyboard, tabUntil } from './support'

test('settings validate, edit, save, back up, and reveal insights by keyboard', async ({ page }, testInfo) => {
  await preparePage(page)
  await page.goto('/settings')
  const count = page.getByLabel('Quantidade de painéis')
  await startKeyboard(page)
  await tabUntil(page, count, testInfo.project.name)
  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.type('0')
  await tabUntil(page, page.getByRole('button', { name: 'Salvar configurações' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(count).toHaveAttribute('aria-invalid', 'true')
  await expect(count).toBeFocused()

  await page.keyboard.press('ControlOrMeta+A')
  await page.keyboard.type('8')
  await tabUntil(page, page.getByRole('button', { name: 'Salvar configurações' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('status', { name: 'Configurações salvas.' })).toBeVisible()

  await startKeyboard(page)
  await tabUntil(page, page.getByRole('button', { name: 'Baixar backup consistente' }), testInfo.project.name)
  const backup = page.waitForEvent('download')
  await page.keyboard.press('Enter')
  await expect((await backup).suggestedFilename()).toBe('helio-backup-20260714-154300.db')

  await startKeyboard(page)
  await tabUntil(page, page.getByRole('link', { name: 'Insights' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('heading', { name: 'Telemetria insuficiente para comparar' })).toBeVisible()
})

test('@screenshot Settings desktop', async ({ page }, testInfo) => {
  test.skip(!isScreenshotProject(testInfo))
  await preparePage(page)
  await page.goto('/settings')
  await expect(page.getByRole('heading', { name: 'O observatório, do seu jeito.' })).toBeVisible()
  await expect(page).toHaveScreenshot('settings-desktop.png', screenshotOptions())
})
