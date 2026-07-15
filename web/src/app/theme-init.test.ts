import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'

describe('theme bootstrap document', () => {
  it('loads a same-origin theme initializer before React and declares Brazilian Portuguese', () => {
    const html = readFileSync('index.html', 'utf8')
    expect(html).toContain('<html lang="pt-BR">')
    expect(html.indexOf('src="/theme-init.js"')).toBeGreaterThan(0)
    expect(html.indexOf('src="/theme-init.js"')).toBeLessThan(html.indexOf('src="/src/main.tsx"'))
  })

  it('falls back safely when browser storage is blocked', () => {
    const script = readFileSync('public/theme-init.js', 'utf8')
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('blocked') })
    vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true })))

    Function(script)()

    expect(document.documentElement.dataset.theme).toBe('dark')
  })
})
