import { useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query'
import { Check, MonitorCog } from 'lucide-react'
import { useEffect, useState } from 'react'

import { ApiError, authMemory } from '../../api/client'
import { componentHealthQuery, login, queryKeys, sessionQuery, settingsQuery, updateSettings } from '../../api/queries'
import type { ComponentHealth, Settings } from '../../api/types'
import { useTheme } from '../../app/theme'
import { ConnectionPanel } from './ConnectionPanel'
import { DataPanel } from './DataPanel'
import { type SettingsErrors, type SettingsField, type SettingsValues, loggerIdentityChanged, settingsServerError, settingsToValues, validateSettings, valuesToSettings } from './settings-model'
import { SystemForm } from './SystemForm'

const themeChoices = [
  { label: 'Sistema', value: 'system' }, { label: 'Claro', value: 'light' }, { label: 'Escuro', value: 'dark' },
] as const

export function SettingsPage() {
  const settings = useQuery(settingsQuery)
  const session = useQuery(sessionQuery)
  const health = useQuery(componentHealthQuery)

  if (settings.isPending) return <section className="settings-state" aria-busy="true"><p>Carregando configurações locais…</p></section>
  if (settings.isError || !settings.data) return <section className="settings-state"><h1>Não foi possível carregar as configurações.</h1><p>Os dados inseridos não foram alterados.</p><button className="secondary-action" onClick={() => { void settings.refetch() }} type="button">Tentar carregar configurações</button></section>

  return <SettingsEditor key={settings.data.loggerHost + settings.data.loggerSerial} health={health} initial={settings.data} sessionUsername={session.data?.username} />
}

function SettingsEditor({ health, initial, sessionUsername }: { health: UseQueryResult<ComponentHealth, Error>; initial: Settings; sessionUsername?: string }) {
  const queryClient = useQueryClient()
  const { setTheme, theme } = useTheme()
  const [original, setOriginal] = useState(initial)
  const [values, setValues] = useState<SettingsValues>(() => settingsToValues(initial))
  const [errors, setErrors] = useState<SettingsErrors>({})
  const [currentPassword, setCurrentPassword] = useState('')
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState('')
  const changedIdentity = loggerIdentityChanged(values, original)

  useEffect(() => () => setCurrentPassword(''), [])

  const setField = (field: SettingsField, value: string) => {
    setValues((current) => ({ ...current, [field]: value }))
    setErrors((current) => ({ ...current, [field]: undefined, general: undefined }))
    setStatus('')
  }
  const toggleMPPT = (input: number) => {
    setValues((current) => ({ ...current, activeMPPT: current.activeMPPT.includes(input) ? current.activeMPPT.filter((item) => item !== input) : [...current.activeMPPT, input].sort() }))
    setErrors((current) => ({ ...current, activeMPPT: undefined, general: undefined }))
  }

  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    if (saving) return
    const nextErrors = validateSettings(values)
    if (changedIdentity && !currentPassword) nextErrors.currentPassword = 'Informe a senha atual para alterar a identidade do logger.'
    if (Object.keys(nextErrors).length > 0) {
      setErrors(nextErrors)
      const first = Object.keys(nextErrors)[0]
      requestAnimationFrame(() => document.getElementById(first)?.focus())
      return
    }
    setSaving(true)
    setStatus('')
    setErrors({})
    try {
      if (changedIdentity) {
        if (!sessionUsername) throw new Error('session unavailable')
        const credentials = await login({ username: sessionUsername, password: currentPassword })
        authMemory.setCSRFToken(credentials.csrfToken)
      }
      const saved = await updateSettings(valuesToSettings(values))
      setOriginal(saved)
      setValues(settingsToValues(saved))
      setCurrentPassword('')
      setStatus('Configurações salvas.')
      await queryClient.invalidateQueries({ queryKey: queryKeys.settings })
      await queryClient.invalidateQueries({ queryKey: queryKeys.live })
      await queryClient.invalidateQueries({ queryKey: queryKeys.health })
    } catch (error) {
      if (error instanceof ApiError && error.status === 422) {
        const [field, message] = settingsServerError(error.message, error.code)
        setErrors({ [field]: message })
        requestAnimationFrame(() => document.getElementById(field)?.focus())
      } else if (error instanceof ApiError && error.status === 409) {
        setErrors({ general: 'As configurações mudaram em outra sessão. Recarregue e tente novamente.' })
      } else if (error instanceof ApiError && error.status === 401) {
        setErrors({ currentPassword: 'A senha atual não foi confirmada. Tente novamente.' })
      } else if (error instanceof TypeError) {
        setErrors({ general: 'Não foi possível alcançar o Helio. Verifique a conexão e tente novamente.' })
      } else {
        setErrors({ general: 'O Helio não conseguiu salvar as configurações. Tente novamente.' })
      }
    } finally {
      setSaving(false)
    }
  }

  return (
    <article className="settings-page">
      <header className="settings-heading"><p className="eyebrow">Configuração local</p><h1>O observatório, do seu jeito.</h1><p>Ajustes ficam neste Helio e neste navegador. Nenhum controle escreve no inversor.</p></header>
      <form id="settings-form" noValidate onSubmit={submit}>
        <SystemForm errors={errors} setField={setField} toggleMPPT={toggleMPPT} values={values} />
        <ConnectionPanel changed={changedIdentity} currentPassword={currentPassword} errors={errors} health={health.data} healthError={health.isError} onPassword={(value) => { setCurrentPassword(value); setErrors((current) => ({ ...current, currentPassword: undefined })) }} retryHealth={() => { void health.refetch() }} setField={setField} values={values} />
      </form>
      <section className="settings-section" aria-labelledby="appearance-title">
        <div className="settings-section-heading"><p className="eyebrow">03 · Aparência</p><h2 id="appearance-title">Luz para cada momento.</h2><p>O modo Sistema acompanha a preferência do dispositivo. A escolha manual fica apenas neste navegador.</p></div>
        <fieldset className="theme-fieldset settings-section-body"><legend className="sr-only">Tema da interface</legend>{themeChoices.map((choice) => <label key={choice.value}><input checked={theme === choice.value} name="settings-theme" onChange={() => setTheme(choice.value)} type="radio" />{choice.label}</label>)}</fieldset>
      </section>
      <DataPanel error={errors.retentionDays} retentionDays={values.retentionDays} setRetentionDays={(value) => setField('retentionDays', value)} />
      <div className="settings-save-rail">
        <div aria-live="polite">{errors.general ? <p className="form-alert">{errors.general}</p> : status ? <p className="save-success"><Check aria-hidden="true" />{status}</p> : <p>Revise os ajustes antes de salvar.</p>}</div>
        <button className="primary-action" disabled={saving} form="settings-form" type="submit">{saving ? 'Salvando configurações…' : 'Salvar configurações'}</button>
      </div>
      <section className="settings-section about-section" aria-labelledby="about-title">
        <div className="settings-section-heading"><p className="eyebrow">05 · Sobre</p><h2 id="about-title">Helio v0.1</h2></div>
        <div className="settings-section-body"><MonitorCog aria-hidden="true" /><p>Monitor solar local e independente. Leituras do logger, histórico em SQLite e análises com evidências — sem comandos Modbus de escrita.</p></div>
      </section>
    </article>
  )
}
