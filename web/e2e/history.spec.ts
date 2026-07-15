import { expect, test } from '@playwright/test'
import { attachScreenshot, expectAccessible, expectNoHorizontalOverflow, preparePage, resetScenario } from './support'

test('history exposes an explicit gap and exports the selected range', async ({ page, request }, testInfo) => {
  await resetScenario(request, 'history-gap')
  await preparePage(page)
  await page.goto('/history')
  await expect(page.getByRole('heading', { name: 'Histórico solar' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Sem dados entre/ })).toBeVisible()
  await expectNoHorizontalOverflow(page)
  await expectAccessible(page)

  const download = page.waitForEvent('download')
  await page.getByRole('link', { name: 'Baixar CSV' }).press('Enter')
  await expect((await download).suggestedFilename()).toBe('helio-history.csv')
  if (testInfo.project.name === 'desktop-chromium') await attachScreenshot(page, testInfo, 'history-gap')
})
