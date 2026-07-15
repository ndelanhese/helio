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
      )
    }
    return data as T
  }
}

export const api = new ApiClient()
