import { describe, expect, it } from 'vitest'

import {
  initialOnboardingValues,
  type OnboardingField,
  type OnboardingValues,
  serverFieldError,
  toBootstrapPayload,
  validateStep,
} from './schema'

const valid: OnboardingValues = {
  ...initialOnboardingValues,
  username: 'Admin',
  password: 'senha segura 123',
  confirmPassword: 'senha segura 123',
  loggerHost: '192.0.2.50',
  loggerSerial: '4294967295',
  latitude: '-10',
  longitude: '-20',
  timezone: 'UTC',
}

describe('onboarding numeric validation', () => {
  it('starts without location coordinates and requires explicit user input', () => {
    expect(initialOnboardingValues.latitude).toBe('')
    expect(initialOnboardingValues.longitude).toBe('')
    expect(validateStep(3, initialOnboardingValues).latitude).toBeDefined()
    expect(validateStep(3, initialOnboardingValues).longitude).toBeDefined()
  })
  it.each([
    [1, 'loggerPort'], [1, 'modbusSlave'], [2, 'panelCount'], [2, 'panelWattage'], [3, 'retentionDays'],
  ] as const)('rejects empty and unsafe integer values for %s', (step, field) => {
    expect(validateStep(step, { ...valid, [field]: '' })[field]).toBeDefined()
    expect(validateStep(step, { ...valid, [field]: '1.5' })[field]).toBeDefined()
    expect(validateStep(step, { ...valid, [field]: '9007199254740992' })[field]).toBeDefined()
  })

  it.each(['latitude', 'longitude', 'tariff'] as const)('rejects empty and non-finite decimals for %s', (field) => {
    expect(validateStep(3, { ...valid, [field]: '' })[field]).toBeDefined()
    expect(validateStep(3, { ...valid, [field]: 'Infinity' })[field]).toBeDefined()
    expect(validateStep(3, { ...valid, [field]: 'NaN' })[field]).toBeDefined()
  })

  it('accepts an explicit zero tariff and serializes finite numbers instead of null defaults', () => {
    const values = { ...valid, tariff: '0' }
    expect(validateStep(3, values).tariff).toBeUndefined()
    expect(toBootstrapPayload(values).settings.tariffMinorPerKWh).toBe(0)
    expect(Object.values(toBootstrapPayload(values).settings).every((value) => value !== null)).toBe(true)
  })

  it.each([
    ['0', 0], ['0.01', 1], ['1.2', 120], ['1,20', 120],
  ] as const)('converts tariff %s to exact minor units', (tariff, minor) => {
    const values = { ...valid, tariff }
    expect(validateStep(3, values).tariff).toBeUndefined()
    expect(toBootstrapPayload(values).settings.tariffMinorPerKWh).toBe(minor)
  })

  it.each(['1.234', '1e2', '10000000.01', '90071992547409.92', '92233720368547758.08'])('rejects unsafe or inexact tariff %s', (tariff) => {
    expect(validateStep(3, { ...valid, tariff }).tariff).toBeDefined()
  })
})

describe('onboarding account and installation validation', () => {
  it('counts password minimum in Unicode code points and maximum in UTF-8 bytes', () => {
    expect(validateStep(0, { ...valid, password: '😀'.repeat(11), confirmPassword: '😀'.repeat(11) }).password).toMatch(/12 caracteres/)
    expect(validateStep(0, { ...valid, password: '😀'.repeat(12), confirmPassword: '😀'.repeat(12) }).password).toBeUndefined()
    expect(validateStep(0, { ...valid, password: 'á'.repeat(65), confirmPassword: 'á'.repeat(65) }).password).toMatch(/128 bytes/)
  })

  it('accepts only a decimal uint32 logger serial', () => {
    expect(validateStep(1, { ...valid, loggerSerial: '4294967295' }).loggerSerial).toBeUndefined()
    expect(validateStep(1, { ...valid, loggerSerial: '4294967296' }).loggerSerial).toMatch(/uint32/i)
    expect(validateStep(1, { ...valid, loggerSerial: '+123' }).loggerSerial).toBeDefined()
    expect(validateStep(1, { ...valid, loggerSerial: '１２３' }).loggerSerial).toBeDefined()
  })

  it.each(['...', '192.0..1', ' 192.0.2.1', '192.0.2.1 ', '+192.0.2.1', '192.0.002.1'])(
    'rejects ambiguous IPv4 text %s',
    (loggerHost) => expect(validateStep(1, { ...valid, loggerHost }).loggerHost).toBeDefined(),
  )

  it('accepts a strict private IPv4 address', () => {
    expect(validateStep(1, { ...valid, loggerHost: '192.0.2.50' }).loggerHost).toBeUndefined()
  })

  it('validates real IANA timezones and ISO currencies', () => {
    expect(validateStep(3, { ...valid, timezone: '' }).timezone).toBeDefined()
    expect(validateStep(3, { ...valid, timezone: 'Local' }).timezone).toBeDefined()
    expect(validateStep(3, { ...valid, timezone: 'Foo/Bar' }).timezone).toBeDefined()
    expect(validateStep(3, { ...valid, timezone: 'America/Sao_Paulo' }).timezone).toBeUndefined()
    expect(validateStep(3, { ...valid, currency: 'ZZZ' }).currency).toBeDefined()
    expect(validateStep(3, { ...valid, currency: 'brl' }).currency).toBeUndefined()
    expect(toBootstrapPayload({ ...valid, currency: 'brl' }).settings.currency).toBe('BRL')
  })

  it.each(['XAU', 'XTS', 'XXX', 'BOV', 'CHE', 'CLF', 'USN'])('accepts backend ISO currency %s regardless of Intl support', (currency) => {
    expect(validateStep(3, { ...valid, currency }).currency).toBeUndefined()
  })

  it('rejects a currency outside the exact backend ISO set', () => {
    expect(validateStep(3, { ...valid, currency: 'ZZZ' }).currency).toBeDefined()
  })
})

describe('bootstrap server error mapping', () => {
  it.each([
    ['invalid_request', 'username is required', 'username'],
    ['invalid_password', 'password rejected', 'password'],
    ['invalid_settings', 'logger host must be an IPv4 address', 'loggerHost'],
    ['invalid_settings', 'logger serial must be a decimal uint32', 'loggerSerial'],
    ['invalid_settings', 'logger port must be between 1 and 65535', 'loggerPort'],
    ['invalid_settings', 'modbus slave must be between 1 and 247', 'modbusSlave'],
    ['invalid_settings', 'panel count must be positive', 'panelCount'],
    ['invalid_settings', 'panel wattage must be positive', 'panelWattage'],
    ['invalid_settings', 'installed power must not exceed 12000 W', 'panelCount'],
    ['invalid_settings', 'at least one MPPT input must be active', 'activeMPPT'],
    ['invalid_settings', 'latitude must be between -90 and 90', 'latitude'],
    ['invalid_settings', 'longitude must be between -180 and 180', 'longitude'],
    ['invalid_settings', 'timezone must be a valid IANA location', 'timezone'],
    ['invalid_settings', 'currency must be an uppercase ISO 4217 code', 'currency'],
    ['invalid_settings', 'tariff must not be negative', 'tariff'],
    ['invalid_settings', 'retention must be between 30 and 3650 days', 'retentionDays'],
  ] as const)('maps %s / %s to %s', (code, message, field) => {
    expect(serverFieldError(message, code)[0]).toBe(field satisfies OnboardingField)
  })

  it('uses a general safe message for an unknown server detail', () => {
    expect(serverFieldError('secret-token=do-not-reflect', 'invalid_settings')).toEqual([
      'general', 'Não foi possível criar o Helio. Revise os dados e tente novamente.',
    ])
  })
})
