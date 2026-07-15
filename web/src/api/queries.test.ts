import { QueryClient } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { authMemory } from './client'
import { sessionQuery } from './queries'

describe('sessionQuery', () => {
  afterEach(() => { authMemory.clear(); vi.unstubAllGlobals() })

  it('keeps the reissued CSRF token only in memory', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      userId: 'u', username: 'Admin', expiresAt: '2026-08-14T00:00:00Z', csrfToken: 'rotated-token',
    }), { headers: { 'Content-Type': 'application/json' } })))
    const client = new QueryClient()

    await client.fetchQuery(sessionQuery)

    expect(authMemory.getCSRFToken()).toBe('rotated-token')
    expect(localStorage.getItem('csrfToken')).toBeNull()
  })
})
