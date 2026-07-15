import { readFile } from 'node:fs/promises'
import { test, expect } from './fixtures'
import { isScreenshotProject, preparePage, screenshotOptions, startKeyboard, tabUntil, waitForVisualReadiness } from './support'

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
  expect(await readFile(await download.path(), 'utf8')).toBe('timestamp,power_w,energy_today_wh,status\n2026-07-14T12:00:00Z,2070,12340,normal\n')
})

test('@screenshot History gap desktop', async ({ page, setScenario }, testInfo) => {
  test.skip(!isScreenshotProject(testInfo))
  await setScenario('history-gap')
  await preparePage(page)
  await page.goto('/history')
  await expect(page.getByRole('heading', { name: 'Histórico solar' })).toBeVisible()
  await waitForVisualReadiness(page, 'light')
  await page.evaluate(() => {
    const chart = document.querySelector<HTMLElement>('.recharts-responsive-container')
    if (!chart) throw new Error('responsive history chart was not rendered')
    const readyWidth = Math.round(chart.getBoundingClientRect().width)
    chart.style.setProperty('width', `${readyWidth}px`, 'important')
    chart.style.setProperty('min-width', `${readyWidth}px`, 'important')
    chart.style.setProperty('max-width', `${readyWidth}px`, 'important')
    const probe = { observer: undefined as ResizeObserver | undefined, widths: [Math.round(chart.getBoundingClientRect().width)] }
    probe.observer = new ResizeObserver(() => probe.widths.push(Math.round(chart.getBoundingClientRect().width)))
    probe.observer.observe(chart)
    ;(window as typeof window & { __helioChartWidthProbe?: typeof probe }).__helioChartWidthProbe = probe
  })
  await page.screenshot(screenshotOptions())
  const chartWidths = await page.evaluate(() => {
    const probe = (window as typeof window & { __helioChartWidthProbe?: { observer?: ResizeObserver; widths: number[] } }).__helioChartWidthProbe
    probe?.observer?.disconnect()
    return probe?.widths ?? []
  })
  expect([...new Set(chartWidths)], `chart widths observed during full-page capture: ${chartWidths.join(', ')}`).toHaveLength(1)
  await expect(page).toHaveScreenshot('history-gap-desktop.png', { ...screenshotOptions(), timeout: 60_000 })
})
