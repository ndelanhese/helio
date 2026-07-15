import { describe, expect, it } from 'vitest'

import { loginRedirect, resolveAppAccess, safeRedirectTarget } from './auth-gate'
import { ApiError } from '../api/client'
import { classifySessionResult } from './auth-gate'

describe('resolveAppAccess', () => {
  it('routes bootstrap, login, and protected destinations from server state', () => {
    expect(resolveAppAccess('/', true, false)).toBe('/bootstrap')
    expect(resolveAppAccess('/history', true, true)).toBe('/bootstrap')
    expect(resolveAppAccess('/login', true, false)).toBe('/bootstrap')
    expect(resolveAppAccess('/bootstrap', false, false)).toBe('/login')
    expect(resolveAppAccess('/bootstrap', false, true)).toBe('/')
    expect(resolveAppAccess('/login', false, false)).toBe('render')
    expect(resolveAppAccess('/history', false, null)).toBe('loading')
    expect(resolveAppAccess('/history', false, false)).toBe('/login')
    expect(resolveAppAccess('/history', false, true)).toBe('render')
  })

  it('preserves only safe same-origin destinations through login', () => {
    expect(loginRedirect('/history?range=week#sun')).toBe('/login?redirect=%2Fhistory%3Frange%3Dweek%23sun')
    expect(safeRedirectTarget('/insights?period=7d#peak')).toBe('/insights?period=7d#peak')
    expect(safeRedirectTarget('https://attacker.test')).toBe('/')
    expect(safeRedirectTarget('//attacker.test')).toBe('/')
    expect(safeRedirectTarget('/\\evil.test')).toBe('/')
    expect(safeRedirectTarget('/%5c%5cevil.test')).toBe('/')
    expect(safeRedirectTarget('/%2f%2fevil.test')).toBe('/')
    expect(safeRedirectTarget('/history/../insights?q=1#ok')).toBe('/insights?q=1#ok')
    expect(safeRedirectTarget('/login')).toBe('/')
    expect(safeRedirectTarget('/bootstrap?next=/history')).toBe('/')
  })
})

describe('classifySessionResult', () => {
  it('treats only a typed 401 as anonymous', () => {
    expect(classifySessionResult(false, new ApiError('unauthorized', 401, 'Sessão encerrada'))).toBe('anonymous')
    expect(classifySessionResult(false, new ApiError('internal_error', 500, 'Falha'))).toBe('unavailable')
    expect(classifySessionResult(false, new TypeError('network down'))).toBe('unavailable')
    expect(classifySessionResult(true, null)).toBe('authenticated')
  })
})
