import { Database, RadioTower, Server, SunMedium } from 'lucide-react'

import type { ComponentHealth } from '../../api/types'
import type { SettingsErrors, SettingsField, SettingsValues } from './settings-model'

interface ConnectionPanelProps {
  changed: boolean
  currentPassword: string
  errors: SettingsErrors
  health?: ComponentHealth
  healthError: boolean
  onPassword: (value: string) => void
  retryHealth: () => void
  setField: (field: SettingsField, value: string) => void
  values: SettingsValues
}

const healthLabels: Record<string, string> = {
  available: 'Disponível', idle: 'Em espera', offline: 'Desconectado', ok: 'Operacional', online: 'Conectado', running: 'Em execução', stale: 'Desatualizado', unavailable: 'Indisponível', unknown: 'Sem leitura',
}

export function ConnectionPanel({ changed, currentPassword, errors, health, healthError, onPassword, retryHealth, setField, values }: ConnectionPanelProps) {
  return (
    <section className="settings-section" aria-labelledby="connection-title">
      <div className="settings-section-heading"><p className="eyebrow">02 · Conexão</p><h2 id="connection-title">Uma leitura local, sem comandos.</h2><p>O Helio permanece somente leitura: consulta o logger e o inversor, sem escrever registradores nem alterar o equipamento.</p></div>
      <div className="settings-section-body">
        <div className="form-fields">
          <Field field="loggerHost" label="Endereço IP do logger" error={errors.loggerHost}><input aria-describedby={errors.loggerHost ? 'loggerHost-error' : undefined} aria-invalid={Boolean(errors.loggerHost)} id="loggerHost" onChange={(event) => setField('loggerHost', event.target.value)} value={values.loggerHost} /></Field>
          <Field field="loggerSerial" label="Número de série do logger" error={errors.loggerSerial}><input aria-describedby={errors.loggerSerial ? 'loggerSerial-error' : undefined} aria-invalid={Boolean(errors.loggerSerial)} autoComplete="off" id="loggerSerial" inputMode="numeric" onChange={(event) => setField('loggerSerial', event.target.value)} value={values.loggerSerial} /></Field>
          <div className="form-pair">
            <Field field="loggerPort" label="Porta do logger" error={errors.loggerPort}><input aria-describedby={errors.loggerPort ? 'loggerPort-error' : undefined} aria-invalid={Boolean(errors.loggerPort)} id="loggerPort" inputMode="numeric" min="1" onChange={(event) => setField('loggerPort', event.target.value)} type="number" value={values.loggerPort} /></Field>
            <Field field="modbusSlave" label="Endereço Modbus" error={errors.modbusSlave}><input aria-describedby={errors.modbusSlave ? 'modbusSlave-error' : undefined} aria-invalid={Boolean(errors.modbusSlave)} id="modbusSlave" inputMode="numeric" min="1" onChange={(event) => setField('modbusSlave', event.target.value)} type="number" value={values.modbusSlave} /></Field>
          </div>
          {changed ? <Field field="currentPassword" label="Senha atual" error={errors.currentPassword}><p className="field-hint" id="currentPassword-hint">Para proteger a conexão local, confirme a senha atual antes de alterar a identidade do logger.</p><input aria-describedby={`currentPassword-hint${errors.currentPassword ? ' currentPassword-error' : ''}`} aria-invalid={Boolean(errors.currentPassword)} autoComplete="current-password" id="currentPassword" onChange={(event) => onPassword(event.target.value)} type="password" value={currentPassword} /></Field> : null}
        </div>
        <section className="component-health" aria-label="Estado da conexão">
          <div className="component-health-heading"><h3>Saúde dos componentes</h3><p>Falhas do logger ou do clima não significam que o processo ou o banco pararam.</p></div>
          {healthError ? <div className="inline-retry"><p>Não foi possível consultar a conexão.</p><button className="secondary-action" onClick={retryHealth} type="button">Tentar consultar conexão</button></div> : health ? <dl>
            <Health icon={Server} label="Processo" value={health.collector} />
            <Health icon={Database} label="Banco de dados" value={health.database} />
            <Health icon={RadioTower} label="Logger" value={health.logger} />
            <Health icon={SunMedium} label="Clima" value={health.weather} />
          </dl> : <p aria-busy="true">Consultando processo, banco, logger e clima…</p>}
        </section>
      </div>
    </section>
  )
}

function Health({ icon: Icon, label, value }: { icon: typeof Server; label: string; value: string }) {
  return <div><dt><Icon aria-hidden="true" />{label}</dt><dd><i className={`health-dot is-${value}`} aria-hidden="true" />{healthLabels[value] ?? value}</dd></div>
}

function Field({ children, error, field, label }: { children: React.ReactNode; error?: string; field: SettingsField | 'currentPassword'; label: string }) {
  return <div className={`form-field${error ? ' has-error' : ''}`}><label htmlFor={field}>{label}</label>{children}{error ? <p className="field-error" id={`${field}-error`}>{error}</p> : null}</div>
}
