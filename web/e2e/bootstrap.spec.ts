import { test, expect } from './fixtures'
import { expectAccessible, preparePage, startKeyboard, tabUntil, TEST_ADMIN, TEST_LOGGER_HOST, TEST_LOGGER_SERIAL, TEST_PASSWORD } from './support'

test('first bootstrap completes every accessible step by keyboard only', async ({ page, setScenario }, testInfo) => {
  await setScenario('bootstrap-open')
  await preparePage(page)
  await page.goto('/history')
  await expect(page).toHaveURL(/\/bootstrap$/)
  await expectAccessible(page)

  await startKeyboard(page)
  await tabUntil(page, page.getByLabel('Usuário administrador'), testInfo.project.name)
  await page.keyboard.type(TEST_ADMIN)
  await tabUntil(page, page.getByLabel('Senha', { exact: true }), testInfo.project.name)
  await page.keyboard.type(TEST_PASSWORD)
  await tabUntil(page, page.getByLabel('Confirmar senha'), testInfo.project.name)
  await page.keyboard.type(TEST_PASSWORD)
  await tabUntil(page, page.getByRole('button', { name: 'Continuar para o logger' }), testInfo.project.name)
  await page.keyboard.press('Enter')

  await expect(page.getByRole('heading', { name: 'Conecte o logger' })).toBeVisible()
  await expectAccessible(page)
  await startKeyboard(page)
  await tabUntil(page, page.getByLabel('Endereço IP do logger'), testInfo.project.name)
  await page.keyboard.type(TEST_LOGGER_HOST)
  await tabUntil(page, page.getByLabel('Número de série do logger'), testInfo.project.name)
  await page.keyboard.type(TEST_LOGGER_SERIAL)
  await tabUntil(page, page.getByRole('button', { name: 'Continuar para os painéis' }), testInfo.project.name)
  await page.keyboard.press('Enter')

  for (const [heading, button] of [
    ['Descreva os painéis', 'Continuar para local e tarifa'],
    ['Local e tarifa', 'Revisar configuração'],
    ['Tudo pronto para começar', 'Criar Helio'],
  ] as const) {
    await expect(page.getByRole('heading', { name: heading })).toBeVisible()
    await expectAccessible(page)
    await startKeyboard(page)
    await tabUntil(page, page.getByRole('button', { name: button }), testInfo.project.name)
    await page.keyboard.press('Enter')
  }

  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
})
