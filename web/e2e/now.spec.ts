import { expect, test } from '@playwright/test'
import { attachScreenshot, expectAccessible, expectNoHorizontalOverflow, expectTouchTargets, preparePage, resetScenario, type TestTheme } from './support'

test('SSE updates power without reload and recovers from a logger outage', async ({ page, request }) => {
  await resetScenario(request)
  await preparePage(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()

  await resetScenario(request, 'next-snapshot')
  await expect(page.getByRole('heading', { name: '2,31 kW' })).toBeVisible()
  await resetScenario(request, 'logger-outage')
  await expect(page.getByRole('status', { name: 'Dados desatualizados' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '2,31 kW' })).toBeVisible()
  await resetScenario(request, 'recovery')
  await expect(page.getByRole('status', { name: 'Ao vivo' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '2,45 kW' })).toBeVisible()
})

for (const viewport of [{ width: 375, height: 812 }, { width: 768, height: 1024 }, { width: 1440, height: 900 }]) {
  for (const theme of ['light', 'dark', 'system'] as TestTheme[]) {
    test(`Now is responsive and accessible at ${viewport.width}x${viewport.height} in ${theme}`, async ({ page, request }) => {
      await resetScenario(request)
      await preparePage(page, theme)
      await page.setViewportSize(viewport)
      await page.goto('/')
      await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
      await expectNoHorizontalOverflow(page)
      await expectTouchTargets(page)
      await expectAccessible(page)
    })
  }
}

test('captures deterministic Now light desktop', async ({ page, request }, testInfo) => {
  test.skip(testInfo.project.name !== 'desktop-chromium')
  await resetScenario(request)
  await preparePage(page, 'light')
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  await attachScreenshot(page, testInfo, 'now-light-desktop')
})

test('captures deterministic Now dark mobile', async ({ page, request }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile-webkit')
  await resetScenario(request)
  await preparePage(page, 'dark')
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  await attachScreenshot(page, testInfo, 'now-dark-mobile')
})
