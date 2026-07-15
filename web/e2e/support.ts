import { expect, type Locator, type Page, type TestInfo } from '@playwright/test'
import { join } from 'node:path'

export const TEST_ADMIN = 'TEST_ADMIN'
export const TEST_PASSWORD = 'Helio-TEST-2026!'
export const TEST_LOGGER_HOST = '192.0.2.44'
export const TEST_LOGGER_SERIAL = '42424242'
export const TEST_CONTROL_TOKEN = 'HELIO-E2E-CONTROL-v1'
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
  await page.addScriptTag({ path: join(process.cwd(), 'node_modules/axe-core/axe.min.js') })
  const violations = await page.evaluate(async () => {
    const axe = (window as unknown as { axe: { run: (root: Document) => Promise<{ violations: Array<{ id: string; impact: string | null; nodes: Array<{ failureSummary: string; html: string; target: string[] }> }> }> } }).axe
    const result = await axe.run(document)
    return result.violations.map(({ id, impact, nodes }) => ({ id, impact, nodes: nodes.map((node) => ({ html: node.html, summary: node.failureSummary, target: node.target })) }))
  })
  expect(violations).toEqual([])
}

export async function expectPageGate(page: Page) {
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
