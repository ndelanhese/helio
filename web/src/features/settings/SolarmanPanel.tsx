import { Cloud, KeyRound, PlugZap, ShieldCheck } from 'lucide-react'
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'

import { ApiError } from '../../api/client'
import { queryKeys, solarmanQuery, testSolarman, updateSolarman } from '../../api/queries'
import type { SolarmanCredentials } from '../../api/types'

const empty: SolarmanCredentials = { appId: '', appSecret: '', account: '', password: '' }

export function SolarmanPanel() {
  const state = useQuery(solarmanQuery)
  const queryClient = useQueryClient()
  const [values, setValues] = useState<SolarmanCredentials>(empty)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [message, setMessage] = useState('')
  const [error, setError] = useState('')
  const ready = values.appId.trim() && values.appSecret.trim() && values.account.trim() && values.password

  const save = async () => {
    if (!ready || saving) return
    setSaving(true); setError(''); setMessage('')
    try {
      const saved = await updateSolarman(values)
      queryClient.setQueryData(queryKeys.solarman, saved)
      setValues(empty)
      setMessage('Credenciais cifradas neste Helio. Teste conexão para confirmar acesso.')
    } catch (cause) { setError(messageFor(cause, 'Não foi possível cifrar credenciais Solarman.')) } finally { setSaving(false) }
  }
  const test = async () => {
    if (testing) return
    setTesting(true); setError(''); setMessage('')
    try {
      const result = await testSolarman()
      setMessage(result.stations.length === 1 ? `Conectado. Estação encontrada: ${result.stations[0].name || result.stations[0].id}.` : `Conectado. ${result.stations.length} estações encontradas.`)
    } catch (cause) { setError(messageFor(cause, 'Solarman não aceitou conexão.')) } finally { setTesting(false) }
  }

  if (state.isPending) return null
  return <section className="settings-section solarman-section" aria-labelledby="solarman-title">
    <div className="settings-section-heading"><p className="eyebrow">04 · Recuperação</p><h2 id="solarman-title">Nuvem só quando faltar dado.</h2><p>Conexão opcional, leitura somente. Helio usa Solarman para recuperar intervalo perdido depois de ficar offline.</p></div>
    <div className="settings-section-body">
      {!state.data?.available ? <div className="solarman-locked"><KeyRound aria-hidden="true" /><div><strong>Chave local ainda não configurada.</strong><p>{state.data?.reason ?? 'Defina HELIO_SECRETS_KEY no arquivo deploy/helio.env e reinicie o Docker.'}</p></div></div> : <>
        {state.data.configured ? <div className="solarman-connected"><ShieldCheck aria-hidden="true" /><div><strong>Conta preparada: {state.data.account}</strong><p>APP ID {state.data.appIdSuffix}. Segredos nunca voltam para tela.</p></div><button className="secondary-action" disabled={testing} onClick={() => { void test() }} type="button"><PlugZap aria-hidden="true" />{testing ? 'Testando…' : 'Testar conexão'}</button></div> : null}
        <div className="solarman-form">
          <div className="form-field"><label htmlFor="solarman-account">Email ou usuário Solarman</label><input autoComplete="username" id="solarman-account" onChange={(event) => setValues((current) => ({ ...current, account: event.target.value }))} value={values.account} /></div>
          <div className="form-field"><label htmlFor="solarman-password">Senha Solarman</label><input autoComplete="current-password" id="solarman-password" onChange={(event) => setValues((current) => ({ ...current, password: event.target.value }))} type="password" value={values.password} /></div>
          <div className="form-field"><label htmlFor="solarman-app-id">APP ID</label><input autoComplete="off" id="solarman-app-id" onChange={(event) => setValues((current) => ({ ...current, appId: event.target.value }))} value={values.appId} /></div>
          <div className="form-field"><label htmlFor="solarman-app-secret">APP Secret</label><input autoComplete="off" id="solarman-app-secret" onChange={(event) => setValues((current) => ({ ...current, appSecret: event.target.value }))} type="password" value={values.appSecret} /></div>
          <div className="solarman-actions"><button className="primary-action" disabled={!ready || saving} onClick={() => { void save() }} type="button"><Cloud aria-hidden="true" />{saving ? 'Cifrando…' : state.data.configured ? 'Trocar credenciais' : 'Salvar credenciais cifradas'}</button><p>Senha e segredo seguem direto para armazenamento cifrado. Nenhum valor aparece depois.</p></div>
        </div>
      </>}
      {(message || error) && <p className={error ? 'form-alert' : 'save-success'} aria-live="polite">{error || message}</p>}
    </div>
  </section>
}

function messageFor(cause: unknown, fallback: string) { if (cause instanceof ApiError && cause.code === 'solarman_unavailable') return 'Defina HELIO_SECRETS_KEY e reinicie Docker antes de salvar.'; return fallback }
