import { describe, expect, it } from 'vitest'

import { resolveAppAccess } from './auth-gate'

describe('resolveAppAccess', () => {
  it('routes bootstrap, login, and protected destinations from server state', () => {
    expect(resolveAppAccess('/', true, false)).toBe('/bootstrap')
    expect(resolveAppAccess('/bootstrap', false, false)).toBe('/login')
    expect(resolveAppAccess('/login', false, false)).toBe('render')
    expect(resolveAppAccess('/history', false, null)).toBe('loading')
    expect(resolveAppAccess('/history', false, false)).toBe('/login')
    expect(resolveAppAccess('/history', false, true)).toBe('render')
  })
})
