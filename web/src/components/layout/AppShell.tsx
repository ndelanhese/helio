import { useState, type ReactNode } from 'react'
import { LogOut } from 'lucide-react'

import { authMemory } from '../../api/client'
import { logout } from '../../api/queries'
import { ConnectionBadge, type ConnectionState } from './ConnectionBadge'
import { PrimaryNav } from './PrimaryNav'
import { ThemeToggle } from './ThemeToggle'

export function AppShell({ announcement, children, connectionState, currentPath }: { announcement?: string; children?: ReactNode; connectionState: ConnectionState; currentPath?: string }) {
  const [loggingOut, setLoggingOut] = useState(false)
  const endSession = async () => {
    if (loggingOut) return
    setLoggingOut(true)
    try {
      await logout()
      authMemory.clear()
      window.location.assign(`/login?redirect=${encodeURIComponent(`${window.location.pathname}${window.location.search}`)}`)
    } catch {
      setLoggingOut(false)
    }
  }
  return (
    <div className="app-shell">
      <a className="skip-link" href="#conteudo">Pular para o conteúdo</a>
      <header className="masthead">
        <a aria-label="Helio — início" className="wordmark" href="/">
          <span>Hélio</span>
          <small>observatório solar</small>
        </a>
        <div className="masthead-actions">
          <ConnectionBadge announcement={announcement} state={connectionState} />
          <ThemeToggle />
          <button aria-label="Sair do Helio" className="icon-button" disabled={loggingOut} onClick={() => { void endSession() }} type="button"><LogOut aria-hidden="true" /></button>
        </div>
      </header>
      <div className="shell-body">
        <PrimaryNav currentPath={currentPath} />
        <main id="conteudo" tabIndex={-1}>{children}</main>
      </div>
    </div>
  )
}
