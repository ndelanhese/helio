import type { ReactNode } from 'react'

import { ConnectionBadge, type ConnectionState } from './ConnectionBadge'
import { PrimaryNav } from './PrimaryNav'
import { ThemeToggle } from './ThemeToggle'

export function AppShell({ announcement, children, connectionState, currentPath }: { announcement?: string; children?: ReactNode; connectionState: ConnectionState; currentPath?: string }) {
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
        </div>
      </header>
      <div className="shell-body">
        <PrimaryNav currentPath={currentPath} />
        <main id="conteudo" tabIndex={-1}>{children}</main>
      </div>
    </div>
  )
}
