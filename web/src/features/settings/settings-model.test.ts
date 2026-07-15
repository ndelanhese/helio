import { describe, expect, it } from 'vitest'

import { HELIO_ISO_4217 } from '../onboarding/currencies'
import { settingsToValues, validateSettings } from './settings-model'

const settings = {
  activeMPPT: [1], currency: 'BRL', installedPowerW: 4_270, latitude: -23.5, longitude: -46.6,
  loggerHost: '192.0.2.50', loggerPort: 8899, loggerSerial: '123', modbusSlave: 1,
  panelCount: 7, panelWattage: 610, retentionDays: 730, tariffMinorPerKWh: 95,
  timezone: 'America/Sao_Paulo',
}

describe('settings currency validation', () => {
  it('accepts every currency in the shared backend contract', () => {
    const values = settingsToValues(settings)
    for (const currency of HELIO_ISO_4217) {
      expect(validateSettings({ ...values, currency }).currency, currency).toBeUndefined()
    }
  })

  it('rejects a three-letter code outside the backend contract', () => {
    const values = settingsToValues(settings)
    expect(validateSettings({ ...values, currency: 'ZZZ' }).currency).toBe('Use uma moeda aceita pelo Helio, como BRL.')
  })
})
