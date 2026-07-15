import { PanelsTopLeft } from 'lucide-react'

import type { SettingsErrors, SettingsField, SettingsValues } from './settings-model'
import { derivedInstalledPower } from './settings-model'

interface SystemFormProps {
  errors: SettingsErrors
  setField: (field: SettingsField, value: string) => void
  toggleMPPT: (input: number) => void
  values: SettingsValues
}

export function SystemForm({ errors, setField, toggleMPPT, values }: SystemFormProps) {
  const power = derivedInstalledPower(values)
  const powerLabel = new Intl.NumberFormat('pt-BR', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(power / 1000)
  return (
    <section className="settings-section" aria-labelledby="system-title">
      <div className="settings-section-heading"><p className="eyebrow">01 · Sistema</p><h2 id="system-title">A instalação como ela é.</h2><p>Esses dados contextualizam a geração. A capacidade é sempre calculada a partir dos painéis informados.</p></div>
      <div className="settings-section-body form-fields">
        <div className="form-pair">
          <Field field="panelCount" label="Quantidade de painéis" error={errors.panelCount}><input aria-describedby={errors.panelCount ? 'panelCount-error' : undefined} aria-invalid={Boolean(errors.panelCount)} id="panelCount" inputMode="numeric" min="1" onChange={(event) => setField('panelCount', event.target.value)} type="number" value={values.panelCount} /></Field>
          <Field field="panelWattage" label="Potência por painel (W)" error={errors.panelWattage}><input aria-describedby={errors.panelWattage ? 'panelWattage-error' : undefined} aria-invalid={Boolean(errors.panelWattage)} id="panelWattage" inputMode="numeric" min="1" onChange={(event) => setField('panelWattage', event.target.value)} type="number" value={values.panelWattage} /></Field>
        </div>
        <output className="capacity-output" aria-live="polite"><PanelsTopLeft aria-hidden="true" /><span>{values.panelCount || '—'} × {values.panelWattage || '—'} W</span><strong>{power > 0 ? `${powerLabel} kWp` : '—'}</strong></output>
        <fieldset className="mppt-fieldset" aria-describedby={errors.activeMPPT ? 'activeMPPT-error' : undefined} aria-invalid={Boolean(errors.activeMPPT)}>
          <legend>Entradas fotovoltaicas ativas</legend>
          {[1, 2].map((input) => <label key={input}><input checked={values.activeMPPT.includes(input)} onChange={() => toggleMPPT(input)} type="checkbox" />PV{input}</label>)}
          {errors.activeMPPT ? <p className="field-error" id="activeMPPT-error">{errors.activeMPPT}</p> : null}
        </fieldset>
        <div className="form-pair">
          <Field field="latitude" label="Latitude" error={errors.latitude}><input aria-describedby={errors.latitude ? 'latitude-error' : undefined} aria-invalid={Boolean(errors.latitude)} id="latitude" inputMode="decimal" onChange={(event) => setField('latitude', event.target.value)} step="any" type="number" value={values.latitude} /></Field>
          <Field field="longitude" label="Longitude" error={errors.longitude}><input aria-describedby={errors.longitude ? 'longitude-error' : undefined} aria-invalid={Boolean(errors.longitude)} id="longitude" inputMode="decimal" onChange={(event) => setField('longitude', event.target.value)} step="any" type="number" value={values.longitude} /></Field>
        </div>
        <div className="form-pair">
          <Field field="timezone" label="Fuso horário IANA" error={errors.timezone}><input aria-describedby={errors.timezone ? 'timezone-error' : undefined} aria-invalid={Boolean(errors.timezone)} id="timezone" onChange={(event) => setField('timezone', event.target.value)} value={values.timezone} /></Field>
          <Field field="tariff" label="Tarifa por kWh" error={errors.tariff}><input aria-describedby={errors.tariff ? 'tariff-error' : undefined} aria-invalid={Boolean(errors.tariff)} id="tariff" inputMode="decimal" onChange={(event) => setField('tariff', event.target.value)} value={values.tariff} /></Field>
        </div>
        <Field field="currency" label="Moeda" error={errors.currency}><input aria-describedby={errors.currency ? 'currency-error' : undefined} aria-invalid={Boolean(errors.currency)} id="currency" maxLength={3} onChange={(event) => setField('currency', event.target.value)} value={values.currency} /></Field>
      </div>
    </section>
  )
}

function Field({ children, error, field, label }: { children: React.ReactNode; error?: string; field: SettingsField; label: string }) {
  return <div className={`form-field${error ? ' has-error' : ''}`}><label htmlFor={field}>{label}</label>{children}{error ? <p className="field-error" id={`${field}-error`}>{error}</p> : null}</div>
}
