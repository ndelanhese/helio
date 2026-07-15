import { createFileRoute } from '@tanstack/react-router'

import { safeRedirectTarget } from '../app/auth-gate'
import { LoginForm } from '../features/auth/LoginForm'

export const Route = createFileRoute('/login')({
  component: LoginRoute,
})

function LoginRoute() {
  const destination = safeRedirectTarget(new URLSearchParams(window.location.search).get('redirect'))
  return (
    <AccessPage kicker="Privado por princípio" note="Dados solares no seu espaço, sem depender da nuvem.">
      <LoginForm onSuccess={() => window.location.assign(destination)} />
    </AccessPage>
  )
}

function AccessPage({ children, kicker, note }: { children: React.ReactNode; kicker: string; note: string }) {
  return (
    <main className="access-page" id="main-content">
      <aside className="access-masthead" aria-label="Helio">
        <a className="access-wordmark" href="/">Helio<span>monitor solar local</span></a>
        <div><p>{kicker}</p><strong>{note}</strong></div>
        <small>Da sua instalação para a sua tela.</small>
      </aside>
      <section className="access-content">{children}</section>
    </main>
  )
}

export { AccessPage }
