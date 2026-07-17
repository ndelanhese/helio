import { test as base, expect, type APIRequestContext } from '@playwright/test'
import { TEST_ADMIN, TEST_CONTROL_TOKEN, TEST_PASSWORD } from './support'

type HelioFixtures = {
  setScenario: (name: string) => Promise<void>
}

async function control(request: APIRequestContext, name: string) {
  const response = await request.post('/__test/scenario', {
    data: { name },
    headers: { 'X-Helio-Test-Token': TEST_CONTROL_TOKEN },
  })
  expect(response.ok(), await response.text()).toBeTruthy()
}

export const test = base.extend<HelioFixtures>({
  setScenario: [async ({ context }, use) => {
    await control(context.request, 'default')
    const login = async () => {
      const response = await context.request.post('/api/v1/auth/login', {
        data: { username: TEST_ADMIN, password: TEST_PASSWORD },
        headers: { Origin: 'http://127.0.0.1:4173' },
      })
      expect(response.status()).toBe(200)
    }
    await login()
    await use(async (name) => {
      await control(context.request, name)
      if (name === 'default' || name === 'history-gap' || name === 'finance') await login()
    })
  }, { auto: true }],
})

export { expect }
