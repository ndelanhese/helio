import { expect, test } from '@playwright/test'

const snapshot = {
  observedAt: '2026-07-14T15:42:00Z', status: 'normal', acPowerW: 2070,
  energyTodayWh: 12340, energyLifetimeWh: 4567800,
  pv1: { active: true, voltageV: 267.1, currentA: 8, powerW: 2070 },
  pv2: { active: false, voltageV: 0, currentA: 0, powerW: 0 },
  grid: { voltageV: 267.1, frequencyHz: 59.97 }, faultCodes: [],
}
const state = { lastSuccess: snapshot.observedAt, stale: false, snapshot }
const settings = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.5, longitude: -46.6,
  loggerHost: '192.168.1.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

for (const viewport of [
  { width: 375, height: 812 },
  { width: 768, height: 1024 },
  { width: 1440, height: 900 },
]) {
  test(`live dashboard has no horizontal overflow at ${viewport.width}x${viewport.height}`, async ({ page }) => {
    await page.setViewportSize(viewport)
    await page.route('**/api/v1/bootstrap/status', (route) => route.fulfill({ json: { open: false } }))
    await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: {
      csrfToken: 'test', expiresAt: '2026-08-14T00:00:00Z', userId: 'test', username: 'Admin',
    } }))
    await page.route('**/api/v1/live', (route) => route.fulfill({ json: state }))
    await page.route('**/api/v1/settings', (route) => route.fulfill({ json: settings }))
    await page.route('**/api/v1/live/events', (route) => route.fulfill({
      body: `event: state\ndata: ${JSON.stringify(state)}\n\n`,
      contentType: 'text/event-stream',
    }))

    await page.goto('/')
    await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
    const layout = await page.evaluate(() => ({
      clientWidth: document.documentElement.clientWidth,
      scrollWidth: document.documentElement.scrollWidth,
    }))
    expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth)
  })
}
