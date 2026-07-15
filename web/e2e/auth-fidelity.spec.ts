import { test, expect } from './fixtures'
import { TEST_ADMIN, TEST_CONTROL_TOKEN, TEST_PASSWORD } from './support'

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
  const login = await anonymous.post('/api/v1/auth/login', { data: { username: TEST_ADMIN, password: TEST_PASSWORD } })
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
  const bootstrap = await context.request.post('/api/v1/bootstrap', { data: {} })
  expect(bootstrap.status()).toBe(409)
  expect(await bootstrap.json()).toEqual({ error: { code: 'bootstrap_closed', message: 'initial setup is already complete' } })
  const csv = await context.request.get('/api/v1/history.csv?from=2026-07-15T00:00:00Z&to=2026-07-14T00:00:00Z')
  expect(csv.status()).toBe(422)
  expect(await csv.json()).toEqual({ error: { code: 'invalid_range', message: 'from and to must define an increasing RFC3339 range' } })
})

test('sessions are isolated across browser contexts', async ({ browser, page }) => {
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  const other = await browser.newContext()
  expect((await other.request.get('/api/v1/auth/session')).status()).toBe(401)
  await other.close()
})
