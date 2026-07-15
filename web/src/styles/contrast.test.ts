import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

describe('editorial contrast tokens', () => {
  it('uses an accessible dark semantic eyebrow in light mode and sun only on dark/masthead contexts', () => {
    const tokens = readFileSync('src/styles/tokens.css', 'utf8')
    const components = readFileSync('src/styles/components.css', 'utf8')
    const global = readFileSync('src/styles/global.css', 'utf8')

    expect(tokens).toMatch(/:root\s*{[^}]*--eyebrow:\s*#173B2D/s)
    expect(tokens).toMatch(/data-theme='dark'[^}]*--eyebrow:\s*#F0C75E/s)
    expect(components).toMatch(/\.eyebrow\s*{[^}]*color:\s*var\(--eyebrow\)/s)
    expect(components).toMatch(/\.access-masthead p\s*{[^}]*color:\s*var\(--sun\)/s)
    expect(global).toMatch(/body\s*{[^}]*color:\s*var\(--text\)/s)
    expect(contrast('#173B2D', '#F3F1E8')).toBeGreaterThanOrEqual(4.5)
  })
})

function contrast(foreground: string, background: string) {
  const luminance = (hex: string) => {
    const channels = hex.slice(1).match(/.{2}/g)?.map((channel) => Number.parseInt(channel, 16) / 255) ?? []
    const [red, green, blue] = channels.map((channel) => channel <= 0.04045 ? channel / 12.92 : ((channel + 0.055) / 1.055) ** 2.4)
    return 0.2126 * red + 0.7152 * green + 0.0722 * blue
  }
  const values = [luminance(foreground), luminance(background)].sort((a, b) => b - a)
  return (values[0] + 0.05) / (values[1] + 0.05)
}
