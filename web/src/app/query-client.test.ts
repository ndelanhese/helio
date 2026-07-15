import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiClient } from '../api/client'
import { replaceLocation } from './navigation'
import './query-client'

vi.mock('./navigation', () => ({ replaceLocation: vi.fn() }))

describe('global unauthorized handler', () => {
  afterEach(() => {
    vi.mocked(replaceLocation).mockClear()
    window.history.replaceState({}, '', '/')
  })

  it('preserves pathname, query, and hash when a protected request becomes unauthorized', async () => {
    window.history.replaceState({}, '', '/history?range=week#x')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'unauthorized', message: 'authentication required' },
    }), { status: 401, headers: { 'Content-Type': 'application/json' } })))

    await expect(new ApiClient().request('/history')).rejects.toMatchObject({ status: 401 })

    expect(replaceLocation).toHaveBeenCalledWith('/login?redirect=%2Fhistory%3Frange%3Dweek%23x')
  })

  it.each(['/login?redirect=%2Fhistory', '/bootstrap'])('does not redirect recursively from %s', async (path) => {
    window.history.replaceState({}, '', path)
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'unauthorized', message: 'authentication required' },
    }), { status: 401, headers: { 'Content-Type': 'application/json' } })))

    await expect(new ApiClient().request('/auth/session')).rejects.toMatchObject({ status: 401 })

    expect(replaceLocation).not.toHaveBeenCalled()
  })
})
