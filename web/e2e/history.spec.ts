import { readFile } from 'node:fs/promises'
import { test, expect } from './fixtures'
import { isScreenshotProject, preparePage, screenshotOptions, startKeyboard, tabUntil } from './support'

test('history exposes a gap and exports the exact selected range by keyboard', async ({ page, setScenario }, testInfo) => {
  await setScenario('history-gap')
  await preparePage(page)
  await page.goto('/history')
  await expect(page.getByRole('heading', { name: 'Histórico solar' })).toBeVisible()
  await expect(page.getByRole('button', { name: /Sem dados entre/ })).toBeVisible()
  const selected = new URL(page.url()).searchParams
  await startKeyboard(page)
  const exportLink = page.getByRole('link', { name: 'Baixar CSV' })
  await tabUntil(page, exportLink, testInfo.project.name)
  const exportURL = new URL(await exportLink.getAttribute('href') ?? '', page.url())
  const downloadPromise = page.waitForEvent('download')
  await page.keyboard.press('Enter')
  const download = await downloadPromise
  const exported = exportURL.searchParams
  expect(exported.get('from')).toBe(selected.get('from'))
  expect(exported.get('to')).toBe(selected.get('to'))
  expect(await readFile(await download.path(), 'utf8')).toBe('timestamp,power_w,energy_wh,status\n2026-07-14T12:00:00Z,2070,3450,ok\n')
})

test('@screenshot History gap desktop', async ({ page, setScenario }, testInfo) => {
  test.skip(!isScreenshotProject(testInfo))
  await setScenario('history-gap')
  await preparePage(page)
  await page.goto('/history')
  await expect(page.getByRole('heading', { name: 'Histórico solar' })).toBeVisible()
  await expect(page).toHaveScreenshot('history-gap-desktop.png', screenshotOptions())
})
