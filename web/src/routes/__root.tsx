import { createRootRoute, Outlet } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'

import { bootstrapStatusQuery, sessionQuery } from '../api/queries'
import { classifySessionResult, loginRedirect, resolveAppAccess } from '../app/auth-gate'
import { replaceLocation } from '../app/navigation'
import { AppShell } from '../components/layout/AppShell'
import { SessionUnavailable } from '../components/layout/SessionUnavailable'

export const Route = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const path = window.location.pathname
  const bootstrap = useQuery(bootstrapStatusQuery)
  const needsSession = bootstrap.data?.open === false && path !== '/login' && path !== '/bootstrap'
  const session = useQuery({ ...sessionQuery, enabled: needsSession })
  const sessionAccess = classifySessionResult(session.isSuccess, session.error)
  const authenticated = sessionAccess === 'authenticated' ? true : sessionAccess === 'anonymous' ? false : null
  const decision = bootstrap.data
    ? resolveAppAccess(path, bootstrap.data.open, needsSession ? authenticated : false)
    : 'loading'

  useEffect(() => {
    if (decision === '/bootstrap') replaceLocation('/bootstrap')
    if (decision === '/login') replaceLocation(loginRedirect(`${path}${window.location.search}`))
  }, [decision, path])

  if (bootstrap.isError) return <main className="route-state">Não foi possível verificar o acesso ao Helio.</main>
  if (needsSession && sessionAccess === 'unavailable') return <SessionUnavailable onRetry={() => { void session.refetch() }} />
  if (decision !== 'render') return <main aria-busy="true" className="route-state">Preparando o Helio…</main>
  if (path === '/login' || path === '/bootstrap') return <Outlet />
  return <AppShell connectionState="unavailable" currentPath={path}><Outlet /></AppShell>
}
