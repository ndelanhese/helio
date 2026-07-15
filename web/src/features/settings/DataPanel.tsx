import { DatabaseBackup } from 'lucide-react'
import { useState } from 'react'

import { downloadBackup } from '../../api/queries'

export function DataPanel({ error, retentionDays, setRetentionDays }: { error?: string; retentionDays: string; setRetentionDays: (value: string) => void }) {
  const [state, setState] = useState<'idle' | 'loading' | 'error' | 'success'>('idle')

  const backup = async () => {
    if (state === 'loading') return
    setState('loading')
    try {
      const { blob, filename } = await downloadBackup()
      const objectURL = URL.createObjectURL(blob)
      const anchor = document.createElement('a')
      anchor.href = objectURL
      anchor.download = filename
      anchor.click()
      URL.revokeObjectURL(objectURL)
      setState('success')
    } catch {
      setState('error')
    }
  }

  return (
    <section className="settings-section" aria-labelledby="data-title">
      <div className="settings-section-heading"><p className="eyebrow">04 · Dados</p><h2 id="data-title">Seu histórico continua portátil.</h2><p>O backup é uma fotografia consistente do banco local, preparada pelo servidor antes do download.</p></div>
      <div className="settings-section-body data-actions">
        <div className={`form-field${error ? ' has-error' : ''}`}><label htmlFor="retentionDays">Retenção do histórico (dias)</label><input aria-describedby={error ? 'retentionDays-error' : undefined} aria-invalid={Boolean(error)} id="retentionDays" inputMode="numeric" min="30" max="3650" onChange={(event) => setRetentionDays(event.target.value)} type="number" value={retentionDays} />{error ? <p className="field-error" id="retentionDays-error">{error}</p> : null}</div>
        <button className="secondary-action" disabled={state === 'loading'} onClick={() => { void backup() }} type="button"><DatabaseBackup aria-hidden="true" />{state === 'loading' ? 'Preparando backup…' : 'Baixar backup consistente'}</button>
        <div aria-live="polite" className="action-status">
          {state === 'error' ? <><p>Não foi possível preparar o backup. Tente novamente.</p><button className="text-action" onClick={() => { void backup() }} type="button">Tentar baixar novamente</button></> : null}
          {state === 'success' ? <p>Backup preparado e enviado ao navegador.</p> : null}
        </div>
      </div>
    </section>
  )
}
