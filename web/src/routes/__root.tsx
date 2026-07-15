import { createRootRoute, Outlet } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useEffect } from 'react'

import { bootstrapStatusQuery, sessionQuery } from '../api/queries'
import { resolveAppAccess } from '../app/auth-gate'
import { AppShell } from '../components/layout/AppShell'

export const Route = createRootRoute({
  component: RootLayout,
})

function RootLayout() {
  const path = window.location.pathname
  const bootstrap = useQuery(bootstrapStatusQuery)
  const needsSession = bootstrap.data?.open === false && path !== '/login' && path !== '/bootstrap'
  const session = useQuery({ ...sessionQuery, enabled: needsSession })
  const decision = bootstrap.data
    ? resolveAppAccess(path, bootstrap.data.open, needsSession ? (session.isPending ? null : session.isSuccess) : false)
    : 'loading'

  useEffect(() => {
    if (decision === '/bootstrap' || decision === '/login') window.location.replace(decision)
  }, [decision])

  if (bootstrap.isError) return <main className="route-state">Não foi possível verificar o acesso ao Helio.</main>
  if (decision !== 'render') return <main aria-busy="true" className="route-state">Preparando o Helio…</main>
  if (path === '/login' || path === '/bootstrap') return <Outlet />
  return <AppShell currentPath={path}><Outlet /></AppShell>
}
