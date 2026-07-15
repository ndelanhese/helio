import { ApiError } from '../api/client'

export type AccessDecision = 'loading' | 'render' | '/bootstrap' | '/login'
export type SessionAccess = 'loading' | 'authenticated' | 'anonymous' | 'unavailable'

export function classifySessionResult(isSuccess: boolean, error: unknown): SessionAccess {
  if (isSuccess) return 'authenticated'
  if (error instanceof ApiError && error.status === 401) return 'anonymous'
  if (error) return 'unavailable'
  return 'loading'
}

export function resolveAppAccess(
  path: string,
  bootstrapOpen: boolean,
  authenticated: boolean | null,
): AccessDecision {
  if (bootstrapOpen) return path === '/bootstrap' ? 'render' : '/bootstrap'
  if (path === '/bootstrap') return '/login'
  if (path === '/login') return 'render'
  if (authenticated === null) return 'loading'
  return authenticated ? 'render' : '/login'
}

export function loginRedirect(destination: string) {
  const safe = safeRedirectTarget(destination)
  return safe === '/' ? '/login' : `/login?redirect=${encodeURIComponent(safe)}`
}

export function safeRedirectTarget(candidate: string | null | undefined) {
  if (!candidate?.startsWith('/') || candidate.startsWith('//')) return '/'
  const path = candidate.split(/[?#]/, 1)[0]
  if (path === '/login' || path === '/bootstrap') return '/'
  return candidate
}
