import type { ApiErrorEnvelope } from './types'
import { replaceLocation } from '../app/navigation'

const API_BASE = '/api/v1'
const MUTATION_METHODS = new Set(['DELETE', 'PATCH', 'POST', 'PUT'])

let csrfToken: string | null = null

export const authMemory = {
  clear: () => { csrfToken = null },
  getCSRFToken: () => csrfToken,
  setCSRFToken: (token: string | null) => { csrfToken = token },
}

export class ApiError extends Error {
  constructor(
    public readonly code: string,
    public readonly status: number,
    message: string,
    public readonly retryAfterSeconds?: number,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

type UnauthorizedHandler = () => void

let handleUnauthorized: UnauthorizedHandler = () => {
  authMemory.clear()
  if (window.location.pathname !== '/login') replaceLocation('/login')
}

export function configureUnauthorizedHandler(handler: UnauthorizedHandler) {
  handleUnauthorized = handler
}

export interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown
}

export class ApiClient {
  constructor(private readonly baseUrl = API_BASE) {}

  async request<T = unknown>(path: string, options: RequestOptions = {}): Promise<T> {
    const method = (options.method ?? 'GET').toUpperCase()
    const headers: Record<string, string> = {
      Accept: 'application/json',
      ...Object.fromEntries(new Headers(options.headers).entries()),
    }
    const hasBody = options.body !== undefined
    if (hasBody) headers['Content-Type'] = 'application/json'

    const currentToken = authMemory.getCSRFToken()
    if (MUTATION_METHODS.has(method) && currentToken) {
      headers['X-CSRF-Token'] = currentToken
    }

    const response = await fetch(`${this.baseUrl}${path}`, {
      ...options,
      body: hasBody ? JSON.stringify(options.body) : undefined,
      credentials: 'same-origin',
      headers,
      method,
    })

    if (response.status === 401) handleUnauthorized()
    if (response.status === 204) return undefined as T

    const contentType = response.headers.get('content-type') ?? ''
    if (!contentType.toLowerCase().includes('application/json')) {
      throw new ApiError('invalid_response', response.ok ? 502 : response.status, 'Resposta inesperada do servidor')
    }

    let data: unknown
    try {
      data = await response.json()
    } catch {
      throw new ApiError('invalid_response', response.ok ? 502 : response.status, 'Resposta JSON inválida do servidor')
    }
    if (!response.ok) {
      const envelope = data as Partial<ApiErrorEnvelope>
      throw new ApiError(
        envelope.error?.code ?? 'request_failed',
        response.status,
        envelope.error?.message ?? 'Não foi possível concluir a solicitação',
        parseRetryAfter(response.headers.get('Retry-After')),
      )
    }
    return data as T
  }

  async download(path: string): Promise<{ blob: Blob; filename: string }> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      credentials: 'same-origin',
      headers: { Accept: 'application/vnd.sqlite3' },
    })
    if (response.status === 401) handleUnauthorized()
    if (!response.ok) {
      let code = 'request_failed'
      try {
        const envelope = await response.json() as Partial<ApiErrorEnvelope>
        code = envelope.error?.code ?? code
      } catch {
        // Binary and empty error responses still use a safe local message.
      }
      throw new ApiError(code, response.status, 'Não foi possível baixar o arquivo')
    }
    const disposition = response.headers.get('Content-Disposition') ?? ''
    const filename = /filename="?([^";]+)"?/i.exec(disposition)?.[1] ?? 'helio-backup.db'
    return { blob: await response.blob(), filename }
  }
}

function parseRetryAfter(value: string | null) {
  if (!value) return undefined
  const seconds = Number.parseInt(value, 10)
  return Number.isFinite(seconds) && seconds > 0 ? seconds : undefined
}

export const api = new ApiClient()
