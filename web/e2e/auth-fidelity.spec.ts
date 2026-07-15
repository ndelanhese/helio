import { test, expect } from './fixtures'
import { TEST_ADMIN, TEST_CONTROL_TOKEN, TEST_PASSWORD, TEST_SETTINGS } from './support'

test('session cookie is HttpOnly, SameSite and persists across reload', async ({ context, page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  const cookie = (await context.cookies()).find((item) => item.name === 'helio_session')
  expect(cookie).toMatchObject({ httpOnly: true, sameSite: 'Strict' })
  expect(cookie?.value).toMatch(/^TEST-OPAQUE-/)
  await page.reload()
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
})

test('protected routes reject no cookie and mutations reject missing or bad CSRF', async ({ browser, request }) => {
  const isolated = await browser.newContext()
  const anonymous = isolated.request
  for (const path of [
    '/api/v1/auth/session', '/api/v1/live', '/api/v1/live/events', '/api/v1/settings',
    '/api/v1/history?from=2026-07-14T00:00:00Z&to=2026-07-15T00:00:00Z&resolution=hour',
    '/api/v1/history.csv?from=2026-07-14T00:00:00Z&to=2026-07-15T00:00:00Z',
    '/api/v1/insights', '/api/v1/alerts?state=open', '/api/v1/data/backup',
  ]) {
    const response = await anonymous.get(path)
    expect(response.status(), path).toBe(401)
    expect(await response.json(), path).toEqual({ error: { code: 'unauthorized', message: 'authentication required' } })
  }
  const login = await anonymous.post('/api/v1/auth/login', {
    data: { username: TEST_ADMIN, password: TEST_PASSWORD }, headers: { Origin: 'http://127.0.0.1:4173' },
  })
  const credentials = await login.json() as { csrfToken: string }
  const origin = 'http://127.0.0.1:4173'
  const missingCSRF = await anonymous.put('/api/v1/settings', { data: {}, headers: { Origin: origin } })
  expect(missingCSRF.status()).toBe(403)
  expect(await missingCSRF.json()).toEqual({ error: { code: 'forbidden', message: 'request origin or CSRF token is invalid' } })
  const badCSRF = await anonymous.put('/api/v1/settings', { data: {}, headers: { Origin: origin, 'X-CSRF-Token': 'TEST-BAD-CSRF' } })
  expect(badCSRF.status()).toBe(403)
  expect(await badCSRF.json()).toEqual({ error: { code: 'forbidden', message: 'request origin or CSRF token is invalid' } })
  expect((await anonymous.post('/api/v1/auth/logout', { headers: { Origin: origin, 'X-CSRF-Token': credentials.csrfToken } })).status()).toBe(204)
  expect((await anonymous.get('/api/v1/live')).status()).toBe(401)
  await isolated.close()

  expect((await request.post('/__test/scenario', { data: { name: 'default' } })).status()).toBe(403)
  expect((await request.post('/__test/scenario', { data: { name: 'default' }, headers: { 'X-Helio-Test-Token': TEST_CONTROL_TOKEN } })).status()).toBe(204)
})

test('closed bootstrap and invalid CSV ranges return exact envelopes', async ({ context }) => {
  const origin = 'http://127.0.0.1:4173'
  const bootstrap = await context.request.post('/api/v1/bootstrap', {
    data: { username: TEST_ADMIN, password: TEST_PASSWORD, settings: TEST_SETTINGS }, headers: { Origin: origin },
  })
  expect(bootstrap.status()).toBe(409)
  expect(await bootstrap.json()).toEqual({ error: { code: 'bootstrap_closed', message: 'initial setup is already complete' } })
  const csv = await context.request.get('/api/v1/history.csv?from=2026-07-15T00:00:00Z&to=2026-07-14T00:00:00Z')
  expect(csv.status()).toBe(422)
  expect(await csv.json()).toEqual({ error: { code: 'invalid_range', message: 'from must be before to and both must be RFC3339 timestamps' } })
  const oversized = await context.request.get('/api/v1/history.csv?from=2026-01-01T00:00:00Z&to=2026-02-02T00:00:00Z')
  expect(oversized.status()).toBe(422)
  expect(await oversized.json()).toEqual({ error: { code: 'invalid_range', message: 'CSV history cannot exceed 31 days' } })
})

test('bootstrap and login reject missing, referer-only, and cross-origin requests', async ({ browser, request }) => {
  const anonymous = await browser.newContext()
  const loginBody = { username: TEST_ADMIN, password: TEST_PASSWORD }
  for (const headers of [{}, { Referer: 'http://127.0.0.1:4173/login' }, { Origin: 'http://evil.invalid' }]) {
    const response = await anonymous.request.post('/api/v1/auth/login', { data: loginBody, headers })
    expect(response.status()).toBe(403)
    expect(await response.json()).toEqual({ error: { code: 'forbidden', message: 'request origin is invalid' } })
  }
  await request.post('/__test/scenario', { data: { name: 'bootstrap-open' }, headers: { 'X-Helio-Test-Token': TEST_CONTROL_TOKEN } })
  for (const headers of [{}, { Referer: 'http://127.0.0.1:4173/bootstrap' }, { Origin: 'http://evil.invalid' }]) {
    const response = await anonymous.request.post('/api/v1/bootstrap', { data: {}, headers })
    expect(response.status()).toBe(403)
    expect(await response.json()).toEqual({ error: { code: 'forbidden', message: 'request origin is invalid' } })
  }
  await anonymous.close()
})

test('settings shape, normalization, strict JSON and validation match production', async ({ context }) => {
  const settings = await context.request.get('/api/v1/settings')
  expect(settings.status()).toBe(200)
  expect(await settings.json()).toMatchObject({ installedPowerW: 4270, panelCount: 7, panelWattage: 610 })
  const session = await context.request.get('/api/v1/auth/session')
  const { csrfToken } = await session.json() as { csrfToken: string }
  const headers = { Origin: 'http://127.0.0.1:4173', 'X-CSRF-Token': csrfToken }
  const empty = await context.request.put('/api/v1/settings', { data: {}, headers })
  expect(empty.status()).toBe(422)
  expect((await empty.json()).error).toMatchObject({ code: 'invalid_settings' })
  const unknown = await context.request.put('/api/v1/settings', { data: { unknown: true }, headers })
  expect(unknown.status()).toBe(400)
  expect(await unknown.json()).toEqual({ error: { code: 'invalid_json', message: 'request body is not valid JSON' } })
  const invalid = await context.request.put('/api/v1/settings', {
    data: { ...TEST_SETTINGS, panelCount: 0 },
    headers,
  })
  expect(invalid.status()).toBe(422)
  expect(await invalid.json()).toEqual({ error: { code: 'invalid_settings', message: 'panel count and wattage must be positive' } })
  const invalidCases = [
    [{ loggerHost: 'not-an-ip' }, 'logger host must be an IPv4 address'],
    [{ loggerPort: 0 }, 'loggerPort must not be zero when provided'],
    [{ loggerSerial: 'not-decimal' }, 'logger serial must be a decimal uint32: strconv.ParseUint: parsing "not-decimal": invalid syntax'],
    [{ modbusSlave: 248 }, 'modbus slave must be between 1 and 247'],
    [{ activeMPPT: [] }, 'at least one MPPT input must be active'],
    [{ latitude: 91 }, 'latitude must be between -90 and 90'],
    [{ longitude: 181 }, 'longitude must be between -180 and 180'],
    [{ timezone: 'Local' }, 'timezone must be a named IANA location'],
    [{ currency: 'brl' }, 'currency must be an uppercase ISO 4217 code'],
    [{ tariffMinorPerKWh: -1 }, 'tariff must not be negative'],
    [{ retentionDays: 29 }, 'retention must be between 30 and 3650 days'],
  ] as const
  for (const [change, message] of invalidCases) {
    const response = await context.request.put('/api/v1/settings', { data: { ...TEST_SETTINGS, ...change }, headers })
    expect(response.status(), JSON.stringify(change)).toBe(422)
    expect(await response.json()).toEqual({ error: { code: 'invalid_settings', message } })
  }
  const saved = await context.request.put('/api/v1/settings', { data: { ...TEST_SETTINGS, panelCount: 8 }, headers })
  expect(saved.status()).toBe(200)
  expect(await saved.json()).toEqual({ ...TEST_SETTINGS, panelCount: 8, installedPowerW: 4880 })
})

test('sessions are isolated across browser contexts', async ({ browser, page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  const other = await browser.newContext()
  expect((await other.request.get('/api/v1/auth/session')).status()).toBe(401)
  await other.close()
})
