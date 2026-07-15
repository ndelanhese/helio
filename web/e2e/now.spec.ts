import { test, expect } from './fixtures'
import { isScreenshotProject, preparePage, screenshotOptions } from './support'

test('SSE updates power and recovers from a logger outage', async ({ page, setScenario }) => {
  await preparePage(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  await setScenario('next-snapshot')
  await expect(page.getByRole('heading', { name: '2,31 kW' })).toBeVisible()
  await setScenario('logger-outage')
  await expect(page.getByRole('status', { name: 'Dados desatualizados' })).toBeVisible()
  await setScenario('recovery')
  await expect(page.getByRole('status', { name: 'Ao vivo' })).toBeVisible()
  await expect(page.getByRole('heading', { name: '2,45 kW' })).toBeVisible()
})

test('@screenshot Now light desktop', async ({ page }, testInfo) => {
  test.skip(!isScreenshotProject(testInfo))
  await preparePage(page, 'light')
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  await expect(page).toHaveScreenshot('now-light-desktop.png', screenshotOptions())
})

test('@screenshot Now dark mobile', async ({ page }, testInfo) => {
  test.skip(!isScreenshotProject(testInfo))
  await page.setViewportSize({ width: 375, height: 812 })
  await preparePage(page, 'dark')
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  await expect(page).toHaveScreenshot('now-dark-mobile.png', screenshotOptions())
})
