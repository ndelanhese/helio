import { expect, type Locator, type Page, type TestInfo } from '@playwright/test'
import { join } from 'node:path'

export const TEST_ADMIN = 'TEST_ADMIN'
export const TEST_PASSWORD = 'Helio-TEST-2026!'
export const TEST_LOGGER_HOST = '192.0.2.44'
export const TEST_LOGGER_SERIAL = '42424242'
export const TEST_CONTROL_TOKEN = 'HELIO-E2E-CONTROL-v1'
export const TEST_SETTINGS = {
  activeMPPT: [1], currency: 'BRL', latitude: -23.55, loggerHost: TEST_LOGGER_HOST, loggerPort: 8899,
  loggerSerial: TEST_LOGGER_SERIAL, longitude: -46.63, modbusSlave: 1, panelCount: 7, panelWattage: 610,
  retentionDays: 730, tariffMinorPerKWh: 95, timezone: 'America/Sao_Paulo',
}
export const FIXED_NOW = new Date('2026-07-14T15:43:00.000Z')

export type TestTheme = 'system' | 'light' | 'dark'

export async function preparePage(page: Page, theme: TestTheme = 'light') {
  await page.clock.install({ time: FIXED_NOW })
  await page.emulateMedia({ colorScheme: theme === 'system' ? 'dark' : theme, reducedMotion: 'reduce' })
  await page.addInitScript((choice) => {
    if (localStorage.getItem('helio.theme.v1') === null) localStorage.setItem('helio.theme.v1', choice)
    const style = document.createElement('style')
    style.textContent = '*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important}'
    document.documentElement.append(style)
  }, theme)
}

export async function expectNoHorizontalOverflow(page: Page) {
  const widths = await page.evaluate(() => ({
    client: document.documentElement.clientWidth,
    scroll: document.documentElement.scrollWidth,
    offenders: [...document.querySelectorAll<HTMLElement>('body *')]
      .filter((element) => element.getBoundingClientRect().right > document.documentElement.clientWidth + 1)
      .slice(0, 8)
      .map((element) => ({ className: element.className.toString(), right: Math.round(element.getBoundingClientRect().right), tag: element.tagName })),
  }))
  expect(widths.scroll, JSON.stringify(widths.offenders)).toBeLessThanOrEqual(widths.client)
}

export async function expectTouchTargets(page: Page) {
  const undersized = await page.evaluate(() => {
    const selector = 'button,a[href],input,select,textarea,[role="checkbox"],[role="radio"],[role="switch"]'
    return [...document.querySelectorAll<HTMLElement>(selector)].flatMap((element) => {
      const style = getComputedStyle(element)
      const hidden = element.hidden || element.getAttribute('aria-hidden') === 'true' || style.display === 'none' || style.visibility === 'hidden'
        || (element instanceof HTMLInputElement && element.type === 'hidden')
      if (hidden || element.getClientRects().length === 0) return []
      let target = element
      if (element instanceof HTMLInputElement && (element.type === 'checkbox' || element.type === 'radio')) target = element.closest('label') ?? element
      const box = target.getBoundingClientRect()
      if (box.width >= 44 && box.height >= 44) return []
      return [{ height: Math.round(box.height), html: element.outerHTML.slice(0, 180), width: Math.round(box.width) }]
    })
  })
  expect(undersized).toEqual([])
}

export async function expectAccessible(page: Page) {
  await waitForVisualReadiness(page)
  await page.addScriptTag({ path: join(process.cwd(), 'node_modules/axe-core/axe.min.js') })
  const violations = await page.evaluate(async () => {
    const axe = (window as unknown as { axe: { run: (root: Document) => Promise<{ violations: Array<{ id: string; impact: string | null; nodes: Array<{ failureSummary: string; html: string; target: string[] }> }> }> } }).axe
    const result = await axe.run(document)
    return result.violations.map(({ id, impact, nodes }) => ({ id, impact, nodes: nodes.map((node) => ({ html: node.html, summary: node.failureSummary, target: node.target })) }))
  })
  expect(violations).toEqual([])
}

export async function waitForVisualReadiness(page: Page, theme?: TestTheme) {
  const expected = theme === 'system' ? 'dark' : theme
  await page.waitForFunction((wanted) => {
    const root = document.documentElement
    const style = getComputedStyle(root)
    const resolved = wanted ?? root.dataset.theme
    const tokens = resolved === 'dark'
      ? { canvas: '#101714', text: '#EEF4EE', background: 'rgb(16, 23, 20)', color: 'rgb(238, 244, 238)' }
      : { canvas: '#F3F1E8', text: '#173B2D', background: 'rgb(243, 241, 232)', color: 'rgb(23, 59, 45)' }
    const linksReady = [...document.querySelectorAll<HTMLLinkElement>('link[rel="stylesheet"]')].every((link) => link.sheet !== null)
    return root.dataset.theme === resolved && linksReady
      && style.getPropertyValue('--canvas').trim().toUpperCase() === tokens.canvas
      && style.getPropertyValue('--text').trim().toUpperCase() === tokens.text
      && style.backgroundColor === tokens.background && style.color === tokens.color
  }, expected)
  await page.evaluate(async () => {
    await document.fonts.ready
    const frames = () => new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())))
    const signature = () => `${document.documentElement.dataset.theme}|${document.styleSheets.length}|${document.body.querySelectorAll('*').length}|${document.body.textContent?.length ?? 0}`
    for (let attempt = 0; attempt < 8; attempt += 1) {
      await frames()
      const before = signature()
      await frames()
      if (before === signature()) return
    }
    throw new Error('application DOM did not stabilize before accessibility analysis')
  })
}

export async function expectPageGate(page: Page, theme?: TestTheme) {
  await waitForVisualReadiness(page, theme)
  await expectNoHorizontalOverflow(page)
  await expectTouchTargets(page)
  await expectAccessible(page)
}

export async function startKeyboard(page: Page) {
  await page.locator('body').waitFor({ state: 'attached' })
  await page.evaluate(() => {
    document.body.tabIndex = -1
    document.body.focus()
  })
  expect(await page.evaluate(() => document.activeElement === document.body)).toBeTruthy()
}

export async function tabUntil(page: Page, target: Locator, projectName: string, direction: 'forward' | 'backward' = 'forward') {
  const visited: string[] = []
  const base = projectName === 'mobile-webkit' ? 'Alt+Tab' : 'Tab'
  const key = direction === 'backward' ? `Shift+${base}` : base
  for (let index = 0; index < 80; index += 1) {
    await page.keyboard.press(key)
    const state = await page.evaluate(() => {
      const active = document.activeElement as HTMLElement | null
      return { label: active?.getAttribute('aria-label') || active?.textContent?.trim() || active?.getAttribute('name') || active?.tagName || '', visible: active?.matches(':focus-visible') ?? false }
    })
    visited.push(state.label.replace(/\s+/g, ' ').slice(0, 100))
    if (await target.evaluate((element) => element === document.activeElement)) {
      await expect(target).toBeFocused()
      expect(state.visible).toBeTruthy()
      return visited
    }
  }
  throw new Error(`Keyboard traversal did not reach target. Visited: ${visited.join(' → ')}`)
}

export function screenshotOptions() {
  return { animations: 'disabled' as const, caret: 'hide' as const, fullPage: true }
}

export function isScreenshotProject(testInfo: TestInfo) {
  return testInfo.project.name === 'desktop-chromium'
}
