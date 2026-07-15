export type AccessDecision = 'loading' | 'render' | '/bootstrap' | '/login'

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
