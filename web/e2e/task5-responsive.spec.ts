import { expect, type Page, test } from '@playwright/test'

const session = {
  csrfToken: 'task5-csrf', expiresAt: '2026-08-14T00:00:00Z', userId: 'task5', username: 'Admin',
}
const settings = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.5, longitude: -46.6,
  loggerHost: '192.168.1.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}
const health = {
  collector: 'running', database: 'ok', logger: 'online', weather: 'stale',
  collectorUpdatedAt: '2026-07-15T02:00:00Z', databaseUpdatedAt: '2026-07-15T02:00:00Z',
  loggerUpdatedAt: '2026-07-15T02:00:00Z', weatherUpdatedAt: '2026-07-15T01:00:00Z',
}
const insight = {
  version: 'v1', day: '2026-07-14', actualWh: 2_000, expectedWh: 10_000, ratio: 0.2,
  confidence: 'medium', qualifying: false,
  evidence: [{ code: 'coverage', label: 'Cobertura da telemetria', value: 42, unit: 'percent' }],
  observationWindow: { qualifyingDays: 4, minimumDays: 7 },
  trends: {
    peakPower: { direction: 'insufficient', current: 0, previous: 0, delta: 0, deltaPct: 0, coveragePct: 42, windowDays: 7 },
    productiveMinutes: { direction: 'insufficient', current: 0, previous: 0, delta: 0, deltaPct: 0, coveragePct: 42, windowDays: 7 },
  },
  generatedEnergyValue: { minor: 190, currency: 'BRL', label: 'valor estimado da energia gerada', estimate: true },
}

async function mockTask5(page: Page) {
  await page.route('**/api/v1/bootstrap/status', (route) => route.fulfill({ json: { open: false } }))
  await page.route('**/api/v1/auth/session', (route) => route.fulfill({ json: session }))
  await page.route('**/api/v1/settings', (route) => route.fulfill({ json: settings }))
  await page.route('**/health/components', (route) => route.fulfill({ json: health }))
  await page.route('**/api/v1/insights?*', (route) => route.fulfill({ json: insight }))
  await page.route('**/api/v1/alerts?*', (route) => {
    const state = new URL(route.request().url()).searchParams.get('state')
    return route.fulfill({ json: { version: 'v1', state, limit: 100, alerts: [] } })
  })
}

async function expectNoHorizontalOverflow(page: Page) {
  const layout = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }))
  expect(layout.scrollWidth).toBeLessThanOrEqual(layout.clientWidth)
}

async function expectTouchTargets(page: Page) {
  const undersized = await page.getByRole('button').evaluateAll((buttons) => buttons
    .filter((button) => button.getClientRects().length > 0)
    .map((button) => ({ label: button.textContent?.trim(), height: button.getBoundingClientRect().height }))
    .filter(({ height }) => height < 44))
  expect(undersized).toEqual([])

  for (const label of ['Quantidade de painéis', 'Potência por painel (W)', 'Endereço IP do logger', 'Número de série do logger', 'Porta do logger', 'Endereço Modbus', 'Fuso horário IANA', 'Tarifa por kWh', 'Moeda', 'Retenção do histórico (dias)']) {
    const control = page.getByLabel(label)
    await expect(control).toBeVisible()
    expect((await control.boundingBox())?.height, label).toBeGreaterThanOrEqual(44)
  }
  for (const label of ['PV1', 'PV2']) {
    const control = page.getByRole('checkbox', { name: label })
    const height = await control.evaluate((input) => input.closest('label')?.getBoundingClientRect().height ?? 0)
    expect(height, label).toBeGreaterThanOrEqual(44)
  }
  for (const label of ['Sistema', 'Claro', 'Escuro']) {
    const control = page.getByRole('radio', { name: label, exact: true })
    const height = await control.evaluate((input) => input.closest('label')?.getBoundingClientRect().height ?? 0)
    expect(height, label).toBeGreaterThanOrEqual(44)
  }
}

for (const viewport of [
  { width: 375, height: 812 },
  { width: 768, height: 1024 },
  { width: 1440, height: 900 },
]) {
  for (const theme of ['light', 'dark'] as const) {
    test(`Settings and Insights remain usable at ${viewport.width}x${viewport.height} in ${theme}`, async ({ page }) => {
      await page.setViewportSize(viewport)
      await page.addInitScript((choice) => localStorage.setItem('helio.theme.v1', choice), theme)
      await mockTask5(page)

      await page.goto('/settings')
      await expect(page.getByRole('heading', { name: 'O observatório, do seu jeito.' })).toBeVisible()
      expect(await page.evaluate(() => document.documentElement.dataset.theme)).toBe(theme)
      await expectNoHorizontalOverflow(page)
      await expectTouchTargets(page)

      await page.goto('/insights')
      await expect(page.getByRole('heading', { name: 'Telemetria insuficiente para comparar' })).toBeVisible()
      await expect(page.getByText(/Este dia não reuniu dados qualificáveis/i)).toBeVisible()
      await expect(page.getByText('Confiança média')).toBeVisible()
      await expect(page.getByText('Cobertura da telemetria')).toBeVisible()
      await expect(page.getByRole('heading', { name: /Produção abaixo da referência aprendida|Produção dentro da faixa observada/ })).toHaveCount(0)
      await expectNoHorizontalOverflow(page)
    })
  }
}

test('wrong current password stays local and preserves the dirty form through keyboard submission', async ({ page }) => {
  await mockTask5(page)
  let settingsWrites = 0
  await page.route('**/api/v1/auth/login', (route) => route.fulfill({
    json: { error: { code: 'invalid_credentials', message: 'invalid credentials' } }, status: 401,
  }))
  await page.route('**/api/v1/settings', (route) => {
    if (route.request().method() === 'PUT') settingsWrites += 1
    return route.fulfill({ json: settings })
  })

  await page.goto('/settings')
  const host = page.getByRole('textbox', { name: 'Endereço IP do logger' })
  await host.fill('192.168.1.60')
  const password = page.getByLabel('Senha atual')
  await password.fill('senha incorreta preservada')
  await password.press('Enter')

  await expect(page.getByText('A senha atual não foi confirmada. Tente novamente.')).toBeVisible()
  await expect(host).toHaveValue('192.168.1.60')
  await expect(password).toHaveValue('senha incorreta preservada')
  await expect(page).toHaveURL(/\/settings$/)
  expect(settingsWrites).toBe(0)
})

test('authenticated backup produces the server filename', async ({ page }) => {
  await mockTask5(page)
  await page.route('**/api/v1/data/backup', (route) => route.fulfill({
    body: 'sqlite-task5', contentType: 'application/octet-stream',
    headers: { 'Content-Disposition': 'attachment; filename="helio-backup-task5.db"' },
  }))
  await page.goto('/settings')

  const downloadPromise = page.waitForEvent('download')
  await page.getByRole('button', { name: 'Baixar backup consistente' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('helio-backup-task5.db')
  await expect(page.getByText('Backup preparado e enviado ao navegador.')).toBeVisible()
})
