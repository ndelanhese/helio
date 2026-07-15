import { expect, type APIRequestContext, type Page, type TestInfo } from '@playwright/test'
import { join } from 'node:path'

// Public, deterministic TEST identifiers. They are intentionally unrelated to a real Helio installation.
export const TEST_ADMIN = 'TEST_ADMIN'
export const TEST_PASSWORD = 'Helio-TEST-2026!'
export const TEST_LOGGER_HOST = '192.0.2.44'
export const TEST_LOGGER_SERIAL = '42424242'
export const FIXED_NOW = new Date('2026-07-14T15:43:00.000Z')

export type TestTheme = 'system' | 'light' | 'dark'

export async function resetScenario(request: APIRequestContext, scenario = 'default') {
  const response = await request.post('/__test/scenario', { data: { name: scenario } })
  expect(response.ok()).toBeTruthy()
}

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
  for (const role of ['button', 'link'] as const) {
    const controls = page.getByRole(role)
    for (let index = 0; index < await controls.count(); index += 1) {
      const control = controls.nth(index)
      if (!(await control.isVisible())) continue
      const box = await control.boundingBox()
      expect(box?.height, `${role} ${await control.getAttribute('aria-label') ?? await control.innerText()}`).toBeGreaterThanOrEqual(44)
      expect(box?.width, `${role} ${await control.getAttribute('aria-label') ?? await control.innerText()}`).toBeGreaterThanOrEqual(44)
    }
  }
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

export async function attachScreenshot(page: Page, testInfo: TestInfo, name: string) {
  await testInfo.attach(name, { body: await page.screenshot({ animations: 'disabled', fullPage: true }), contentType: 'image/png' })
}
