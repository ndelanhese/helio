import { test, expect } from './fixtures'
import { preparePage, startKeyboard, tabUntil } from './support'

test('theme choices persist across reloads by keyboard only', async ({ page }, testInfo) => {
  await preparePage(page, 'system')
  await page.goto('/')
  await startKeyboard(page)
  await tabUntil(page, page.getByRole('button', { name: 'Tema: sistema' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('menuitemradio', { name: 'Sistema' })).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(page.getByRole('menuitemradio', { name: 'Escuro' })).toBeFocused()
  await page.keyboard.press('Enter')
  await page.reload()
  await expect(page.getByRole('button', { name: 'Tema: escuro' })).toBeVisible()
})

test('keyboard-only navigation reaches content and every primary destination', async ({ page }, testInfo) => {
  await preparePage(page)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: '2,07 kW' })).toBeVisible()
  await startKeyboard(page)
  await tabUntil(page, page.getByRole('link', { name: 'Pular para o conteúdo' }), testInfo.project.name)
  await page.keyboard.press('Enter')
  await expect(page.getByRole('main')).toBeFocused()

  for (const [destination, path] of [['Histórico', '/history'], ['Insights', '/insights'], ['Configurações', '/settings']] as const) {
    await startKeyboard(page)
    const visited = await tabUntil(page, page.getByRole('link', { name: destination }), testInfo.project.name)
    expect(visited.length).toBeGreaterThan(0)
    await page.keyboard.press('Enter')
    await expect(page).toHaveURL(new RegExp(`${path}$`))
  }
})
