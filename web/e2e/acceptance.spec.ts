import { test, expect } from './fixtures'
import { expectPageGate, preparePage, type TestTheme } from './support'

const viewports = [{ width: 375, height: 812 }, { width: 768, height: 1024 }, { width: 1440, height: 900 }]
const themes: TestTheme[] = ['light', 'dark', 'system']
const pages = [
  { name: 'Now', path: '/', heading: '2,07 kW' },
  { name: 'History', path: '/history', heading: 'Histórico solar' },
  { name: 'Settings', path: '/settings', heading: 'O observatório, do seu jeito.' },
  { name: 'Insights', path: '/insights', heading: 'Telemetria insuficiente para comparar' },
] as const

for (const destination of pages) for (const viewport of viewports) for (const theme of themes) {
  test(`${destination.name} gate ${viewport.width}x${viewport.height} ${theme}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await preparePage(page, theme)
    await page.goto(destination.path)
    await expect(page.getByRole('heading', { name: destination.heading })).toBeVisible()
    await expectPageGate(page, theme)
  })
}

for (const viewport of viewports) for (const theme of themes) {
  test(`Login gate ${viewport.width}x${viewport.height} ${theme}`, async ({ context, page }) => {
    await context.clearCookies()
    await page.setViewportSize(viewport)
    await preparePage(page, theme)
    await page.goto('/login')
    await expect(page.getByRole('heading', { name: 'Entre no seu Helio' })).toBeVisible()
    await expectPageGate(page, theme)
  })

  test(`Bootstrap gate ${viewport.width}x${viewport.height} ${theme}`, async ({ page, setScenario }) => {
    await setScenario('bootstrap-open')
    await page.setViewportSize(viewport)
    await preparePage(page, theme)
    await page.goto('/bootstrap')
    await expect(page.getByRole('heading', { name: 'Crie a conta local' })).toBeVisible()
    await expectPageGate(page, theme)
  })
}
