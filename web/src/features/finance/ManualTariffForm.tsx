import { useState } from 'react'

import type { ManualTariffInput } from '../../api/types'

type Values = Record<'distributor' | 'effectiveFrom' | 'effectiveTo' | 'consumptionTE' | 'consumptionTUSD' | 'compensationTE' | 'compensationTUSD' | 'flag' | 'availability' | 'cip', string>
const initial: Values = { distributor: 'COPEL', effectiveFrom: '', effectiveTo: '', consumptionTE: '', consumptionTUSD: '', compensationTE: '', compensationTUSD: '', flag: '', availability: '100', cip: '' }

function micros(value: string) {
  const match = /^(\d+)(?:[.,](\d{1,6}))?$/.exec(value.trim())
  if (!match) return undefined
  const parsed = BigInt(match[1]) * 1_000_000n + BigInt((match[2] ?? '').padEnd(6, '0'))
  return parsed <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(parsed) : undefined
}
function minor(value: string) {
  const match = /^(\d+)(?:[.,](\d{1,2}))?$/.exec(value.trim())
  if (!match) return undefined
  const parsed = BigInt(match[1]) * 100n + BigInt((match[2] ?? '').padEnd(2, '0'))
  return parsed <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(parsed) : undefined
}

export function ManualTariffForm({ onSave }: { onSave: (payload: ManualTariffInput) => Promise<void> }) {
  const [values, setValues] = useState(initial); const [error, setError] = useState(''); const [saving, setSaving] = useState(false)
  const set = (key: keyof Values, value: string) => { setValues({ ...values, [key]: value }); setError('') }
  const submit = async (event: React.FormEvent) => {
    event.preventDefault()
    const [consumptionTE, consumptionTUSD, compensationTE, compensationTUSD, flag] = [micros(values.consumptionTE), micros(values.consumptionTUSD), micros(values.compensationTE), micros(values.compensationTUSD), micros(values.flag)]
    const cip = minor(values.cip); const availability = Number(values.availability)
    if (!values.distributor.trim() || !values.effectiveFrom || !values.effectiveTo || consumptionTE === undefined || consumptionTUSD === undefined || compensationTE === undefined || compensationTUSD === undefined || flag === undefined || cip === undefined || ![30, 50, 100].includes(availability)) { setError('Informe datas, valores positivos ou zero, CIP e tipo de ligação.'); return }
    setSaving(true)
    try {
      await onSave({ distributor: values.distributor.trim(), effectiveFrom: values.effectiveFrom, effectiveTo: values.effectiveTo, consumptionTEMicrosPerKWh: consumptionTE, consumptionTUSDMicrosPerKWh: consumptionTUSD, compensationTEMicrosPerKWh: compensationTE, compensationTUSDMicrosPerKWh: compensationTUSD, flagMicrosPerKWh: flag, availabilityKWh: availability, cipMinor: cip })
      setValues(initial)
    } catch { setError('Não foi possível criar a proposta. Revise os valores da fatura.') } finally { setSaving(false) }
  }
  return <section className="tariff-editor"><p className="eyebrow">Tarifa detalhada</p><h2>Conferir pela fatura</h2><p>Use valores unitários da conta. Para sua ligação trifásica, selecione 100 kWh. Bandeira aqui é valor líquido por kWh; deixe ajuste avulso da fatura em zero. A aprovação ainda será explícita.</p><form onSubmit={(event) => { void submit(event) }}><div className="finance-fields"><label>Distribuidora<input value={values.distributor} onChange={(event) => set('distributor', event.target.value)} /></label><label>Vigência inicial<input type="date" value={values.effectiveFrom} onChange={(event) => set('effectiveFrom', event.target.value)} /></label><label>Vigência final<input type="date" value={values.effectiveTo} onChange={(event) => set('effectiveTo', event.target.value)} /></label><label>TE consumo (R$/kWh)<input inputMode="decimal" placeholder="0,389503" value={values.consumptionTE} onChange={(event) => set('consumptionTE', event.target.value)} /></label><label>TUSD consumo (R$/kWh)<input inputMode="decimal" placeholder="0,538944" value={values.consumptionTUSD} onChange={(event) => set('consumptionTUSD', event.target.value)} /></label><label>TE compensação (R$/kWh)<input inputMode="decimal" placeholder="0,389506" value={values.compensationTE} onChange={(event) => set('compensationTE', event.target.value)} /></label><label>TUSD compensação (R$/kWh)<input inputMode="decimal" placeholder="0,319588" value={values.compensationTUSD} onChange={(event) => set('compensationTUSD', event.target.value)} /></label><label>Bandeira líquida (R$/kWh)<input inputMode="decimal" placeholder="0,004938" value={values.flag} onChange={(event) => set('flag', event.target.value)} /></label><label>Ligação<select value={values.availability} onChange={(event) => set('availability', event.target.value)}><option value="30">Monofásica · 30 kWh</option><option value="50">Bifásica · 50 kWh</option><option value="100">Trifásica · 100 kWh</option></select></label><label>CIP mensal (R$)<input inputMode="decimal" placeholder="25,56" value={values.cip} onChange={(event) => set('cip', event.target.value)} /></label></div>{error ? <p className="form-alert" role="alert">{error}</p> : null}<button className="primary-action" disabled={saving} type="submit">{saving ? 'Criando proposta…' : 'Criar proposta de tarifa'}</button></form></section>
}
