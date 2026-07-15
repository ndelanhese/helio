import { createRootRoute, Outlet } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'

import { bootstrapStatusQuery, sessionQuery } from '../api/queries'
import { classifySessionResult, loginRedirect, resolveAppAccess } from '../app/auth-gate'
import { replaceLocation } from '../app/navigation'
import { AppShell } from '../components/layout/AppShell'
import type { ConnectionState } from '../components/layout/ConnectionBadge'
import { SessionUnavailable } from '../components/layout/SessionUnavailable'
import { useLiveStatus } from '../features/live/useLiveTelemetry'

export const Route = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const liveStatus = useLiveStatus()
  const path = window.location.pathname
  const bootstrap = useQuery(bootstrapStatusQuery)
  const needsSession = bootstrap.data?.open === false && path !== '/login' && path !== '/bootstrap'
  const session = useQuery({ ...sessionQuery, enabled: needsSession })
  const sessionAccess = classifySessionResult(session.isSuccess, session.error)
  const authenticated = sessionAccess === 'authenticated' ? true : sessionAccess === 'anonymous' ? false : null
  const decision = bootstrap.data
    ? resolveAppAccess(path, bootstrap.data.open, needsSession ? authenticated : false)
    : 'loading'
  const shellConnection: ConnectionState = path === '/' ? liveStatus.connectionState : 'unavailable'

  useEffect(() => {
    if (decision === '/bootstrap') replaceLocation('/bootstrap')
    if (decision === '/login') replaceLocation(loginRedirect(`${path}${window.location.search}`))
  }, [decision, path])

  if (bootstrap.isError) return <main className="route-state">Não foi possível verificar o acesso ao Helio.</main>
  if (needsSession && sessionAccess === 'unavailable') return <SessionUnavailable onRetry={() => { void session.refetch() }} />
  if (decision !== 'render') return <main aria-busy="true" className="route-state">Preparando o Helio…</main>
  if (path === '/login' || path === '/bootstrap') return <Outlet />
  return <AppShell announcement={path === '/' ? liveStatus.announcement : undefined} connectionState={shellConnection} currentPath={path}><Outlet /></AppShell>
}
