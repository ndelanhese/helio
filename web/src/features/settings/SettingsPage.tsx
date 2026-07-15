import { useQuery, useQueryClient, type UseQueryResult } from '@tanstack/react-query'
import { Check, MonitorCog } from 'lucide-react'
import { useEffect, useState } from 'react'

import { ApiError } from '../../api/client'
import { componentHealthQuery, confirmCurrentPassword, queryKeys, sessionQuery, settingsQuery, updateSettings } from '../../api/queries'
import type { ComponentHealth, Settings } from '../../api/types'
import { useTheme } from '../../app/theme'
import { ConnectionPanel } from './ConnectionPanel'
import { DataPanel } from './DataPanel'
import { type SettingsErrors, type SettingsField, type SettingsValues, loggerIdentityChanged, sameSettings, sameSettingsValues, settingsServerError, settingsToValues, validateSettings, valuesToSettings } from './settings-model'
import { SystemForm } from './SystemForm'

const themeChoices = [
  { label: 'Sistema', value: 'system' }, { label: 'Claro', value: 'light' }, { label: 'Escuro', value: 'dark' },
] as const

export function SettingsPage() {
  const settings = useQuery(settingsQuery)
  useQuery(sessionQuery)
  const health = useQuery(componentHealthQuery)

  if (settings.isPending) return <section className="settings-state" aria-busy="true"><p>Carregando configurações locais…</p></section>
  if (settings.isError || !settings.data) return <section className="settings-state"><h1>Não foi possível carregar as configurações.</h1><p>Os dados inseridos não foram alterados.</p><button className="secondary-action" onClick={() => { void settings.refetch() }} type="button">Tentar carregar configurações</button></section>

  return <SettingsEditor health={health} initial={settings.data} />
}

function SettingsEditor({ health, initial }: { health: UseQueryResult<ComponentHealth, Error>; initial: Settings }) {
  const queryClient = useQueryClient()
  const { setTheme, theme } = useTheme()
  const [original, setOriginal] = useState(initial)
  const [values, setValues] = useState<SettingsValues>(() => settingsToValues(initial))
  const [errors, setErrors] = useState<SettingsErrors>({})
  const [currentPassword, setCurrentPassword] = useState('')
  const [conflict, setConflict] = useState<Settings | null>(null)
  const [saving, setSaving] = useState(false)
  const [status, setStatus] = useState('')
  const dirty = !sameSettingsValues(values, settingsToValues(original))
  const changedIdentity = loggerIdentityChanged(values, original)

  useEffect(() => () => setCurrentPassword(''), [])
  useEffect(() => {
    if (sameSettings(initial, original)) return
    if (dirty || saving) {
      setConflict(initial)
      return
    }
    setOriginal(initial)
    setValues(settingsToValues(initial))
    setConflict(null)
    setErrors({})
  }, [dirty, initial, original, saving])

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
        await confirmCurrentPassword({ password: currentPassword })
      }
      const saved = await updateSettings(valuesToSettings(values))
      queryClient.setQueryData(queryKeys.settings, saved)
      setOriginal(saved)
      setValues(settingsToValues(saved))
      setCurrentPassword('')
      setConflict(null)
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
        <section className="settings-section" aria-labelledby="appearance-title">
        <div className="settings-section-heading"><p className="eyebrow">03 · Aparência</p><h2 id="appearance-title">Luz para cada momento.</h2><p>O modo Sistema acompanha a preferência do dispositivo. A escolha manual fica apenas neste navegador.</p></div>
        <fieldset className="theme-fieldset settings-section-body"><legend className="sr-only">Tema da interface</legend>{themeChoices.map((choice) => <label key={choice.value}><input checked={theme === choice.value} name="settings-theme" onChange={() => setTheme(choice.value)} type="radio" />{choice.label}</label>)}</fieldset>
        </section>
        <DataPanel error={errors.retentionDays} retentionDays={values.retentionDays} setRetentionDays={(value) => setField('retentionDays', value)} />
        {conflict ? <section aria-label="Configurações alteradas no servidor" aria-live="polite" className="settings-conflict" role="status">
        <div><strong>As configurações foram alteradas em outra sessão.</strong><p>{saving ? 'Aguarde o salvamento terminar para escolher quais valores devem permanecer.' : 'Escolha quais valores devem permanecer antes de continuar.'}</p></div>
        <div className="conflict-actions">
          <button className="secondary-action" disabled={saving} onClick={() => {
            setOriginal(conflict)
            setValues(settingsToValues(conflict))
            setCurrentPassword('')
            setConflict(null)
            setErrors({})
            setStatus('Alterações do servidor carregadas.')
          }} type="button">Carregar alterações do servidor</button>
          <button className="secondary-action" disabled={saving} onClick={() => {
            setOriginal(conflict)
            setConflict(null)
            setStatus('Suas edições foram mantidas.')
          }} type="button">Manter minhas edições</button>
        </div>
        </section> : null}
        <div className="settings-save-rail">
        <div aria-label={errors.general || status || 'Estado do salvamento'} aria-live="polite" role="status">{errors.general ? <p className="form-alert">{errors.general}</p> : status ? <p className="save-success"><Check aria-hidden="true" />{status}</p> : <p>Revise os ajustes antes de salvar.</p>}</div>
          <button className="primary-action" disabled={saving} type="submit">{saving ? 'Salvando configurações…' : 'Salvar configurações'}</button>
        </div>
      </form>
      <section className="settings-section about-section" aria-labelledby="about-title">
        <div className="settings-section-heading"><p className="eyebrow">05 · Sobre</p><h2 id="about-title">Helio v0.1</h2></div>
        <div className="settings-section-body"><MonitorCog aria-hidden="true" /><p>Monitor solar local e independente. Leituras do logger, histórico em SQLite e análises com evidências — sem comandos Modbus de escrita.</p></div>
      </section>
    </article>
  )
}
