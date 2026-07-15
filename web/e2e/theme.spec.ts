import { expect, test } from '@playwright/test'
import { preparePage, resetScenario } from './support'

test('system to dark to light theme choices persist across reloads', async ({ page, request }) => {
  await resetScenario(request)
  await preparePage(page, 'system')
  await page.goto('/')

  await page.getByRole('button', { name: 'Tema: sistema' }).press('Enter')
  await page.getByRole('menuitemradio', { name: 'Escuro' }).press('Enter')
  await page.reload()
  await expect(page.getByRole('button', { name: 'Tema: escuro' })).toBeVisible()
  await page.getByRole('button', { name: 'Tema: escuro' }).press('Enter')
  await page.getByRole('menuitemradio', { name: 'Claro' }).press('Enter')
  await page.reload()
  await expect(page.getByRole('button', { name: 'Tema: claro' })).toBeVisible()
})

test('keyboard-only navigation reaches content and every primary destination', async ({ page, request }, testInfo) => {
  await resetScenario(request)
  await preparePage(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()

  await page.keyboard.press(testInfo.project.name === 'mobile-webkit' ? 'Alt+Tab' : 'Tab')
  await expect(page.getByRole('link', { name: 'Pular para o conteúdo' })).toBeFocused()
  await page.getByRole('link', { name: 'Pular para o conteúdo' }).press('Enter')
  await expect(page.getByRole('main')).toBeFocused()
  for (const destination of ['Histórico', 'Insights', 'Configurações']) {
    await page.getByRole('link', { name: destination }).press('Enter')
    await expect(page.getByRole('link', { name: destination })).toHaveAttribute('aria-current', 'page')
  }
  await expect(page.getByRole('heading', { name: 'O observatório, do seu jeito.' })).toBeVisible()
})
