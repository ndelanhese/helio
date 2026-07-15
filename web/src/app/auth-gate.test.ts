import { describe, expect, it } from 'vitest'

import { resolveAppAccess } from './auth-gate'
import { ApiError } from '../api/client'
import { classifySessionResult } from './auth-gate'

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

describe('classifySessionResult', () => {
  it('treats only a typed 401 as anonymous', () => {
    expect(classifySessionResult(false, new ApiError('unauthorized', 401, 'Sessão encerrada'))).toBe('anonymous')
    expect(classifySessionResult(false, new ApiError('internal_error', 500, 'Falha'))).toBe('unavailable')
    expect(classifySessionResult(false, new TypeError('network down'))).toBe('unavailable')
    expect(classifySessionResult(true, null)).toBe('authenticated')
  })
})
