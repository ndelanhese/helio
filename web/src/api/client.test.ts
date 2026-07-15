import { afterEach, describe, expect, it, vi } from 'vitest'

import { ApiClient, ApiError, authMemory, configureUnauthorizedHandler } from './client'

describe('ApiClient', () => {
  afterEach(() => {
    authMemory.clear()
    configureUnauthorizedHandler(() => { authMemory.clear() })
    vi.unstubAllGlobals()
  })

  it('uses the API base and same-origin browser session', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ open: false }), {
      headers: { 'Content-Type': 'application/json' },
    }))
    vi.stubGlobal('fetch', fetchMock)

    await new ApiClient().request('/bootstrap/status')

    expect(fetchMock).toHaveBeenCalledWith('/api/v1/bootstrap/status', expect.objectContaining({
      credentials: 'same-origin',
    }))
  })

  it('reads the current in-memory CSRF token for mutations', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)
    authMemory.setCSRFToken('fresh-token')

    await new ApiClient().request('/auth/logout', { method: 'POST' })

    expect(fetchMock.mock.calls[0]?.[1]?.headers).toEqual(expect.objectContaining({
      'X-CSRF-Token': 'fresh-token',
    }))
  })

  it('throws the typed server error envelope', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'invalid_request', message: 'Dados inválidos' },
    }), { status: 422, headers: { 'Content-Type': 'application/json' } })))

    await expect(new ApiClient().request('/settings')).rejects.toEqual(
      new ApiError('invalid_request', 422, 'Dados inválidos'),
    )
  })

  it('normalizes malformed JSON as a typed response error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('{', {
      status: 502,
      headers: { 'Content-Type': 'application/json' },
    })))

    await expect(new ApiClient().request('/live')).rejects.toEqual(
      new ApiError('invalid_response', 502, 'Resposta JSON inválida do servidor'),
    )
  })

  it('keeps an expected credential-confirmation 401 local', async () => {
    const unauthorized = vi.fn()
    configureUnauthorizedHandler(unauthorized)
    authMemory.setCSRFToken('session-csrf')
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({
      error: { code: 'invalid_credentials', message: 'invalid' },
    }), { status: 401, headers: { 'Content-Type': 'application/json' } })))

    await expect(new ApiClient().request('/auth/login', {
      body: { username: 'Admin', password: 'wrong' },
      method: 'POST',
      suppressUnauthorized: true,
    })).rejects.toEqual(new ApiError('invalid_credentials', 401, 'invalid'))
    expect(unauthorized).not.toHaveBeenCalled()
    expect(authMemory.getCSRFToken()).toBe('session-csrf')
  })

  it('still applies the global unauthorized path to settings mutations and backup', async () => {
    const unauthorized = vi.fn()
    configureUnauthorizedHandler(unauthorized)
    const response = () => new Response(JSON.stringify({ error: { code: 'unauthorized', message: 'expired' } }), {
      status: 401, headers: { 'Content-Type': 'application/json' },
    })
    vi.stubGlobal('fetch', vi.fn().mockImplementation(response))
    const client = new ApiClient()

    await expect(client.request('/settings', { method: 'PUT', body: {} })).rejects.toBeInstanceOf(ApiError)
    await expect(client.download('/data/backup')).rejects.toBeInstanceOf(ApiError)
    expect(unauthorized).toHaveBeenCalledTimes(2)
  })
})
