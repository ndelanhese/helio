import type { Settings } from '../../api/types'
import { HELIO_ISO_4217_SET } from '../onboarding/currencies'
import { serverFieldError } from '../onboarding/schema'

export interface SettingsValues {
  activeMPPT: number[]
  currency: string
  latitude: string
  loggerHost: string
  loggerPort: string
  loggerSerial: string
  longitude: string
  modbusSlave: string
  panelCount: string
  panelWattage: string
  retentionDays: string
  tariff: string
  timezone: string
}

export type SettingsField = keyof SettingsValues
export type SettingsErrors = Partial<Record<SettingsField | 'currentPassword' | 'general', string>>

function integer(value: string) {
  if (!/^\d+$/.test(value)) return undefined
  const parsed = Number(value)
  return Number.isSafeInteger(parsed) ? parsed : undefined
}

function decimal(value: string) {
  const normalized = value.trim().replace(',', '.')
  if (!/^[+-]?(?:\d+(?:\.\d*)?|\.\d+)$/.test(normalized)) return undefined
  const parsed = Number(normalized)
  return Number.isFinite(parsed) ? parsed : undefined
}

function tariffMinor(value: string) {
  const match = /^(\d+)(?:[.,](\d{1,2}))?$/.exec(value.trim())
  if (!match) return undefined
  const minor = BigInt(match[1]) * 100n + BigInt((match[2] ?? '').padEnd(2, '0') || '0')
  return minor <= 1_000_000_000n ? Number(minor) : undefined
}

export function settingsToValues(settings: Settings): SettingsValues {
  const tariffMajor = Math.floor(settings.tariffMinorPerKWh / 100)
  const tariffFraction = String(settings.tariffMinorPerKWh % 100).padStart(2, '0')
  return {
    activeMPPT: [...settings.activeMPPT], currency: settings.currency,
    latitude: String(settings.latitude), loggerHost: settings.loggerHost,
    loggerPort: String(settings.loggerPort), loggerSerial: settings.loggerSerial,
    longitude: String(settings.longitude), modbusSlave: String(settings.modbusSlave),
    panelCount: String(settings.panelCount), panelWattage: String(settings.panelWattage),
    retentionDays: String(settings.retentionDays), tariff: `${tariffMajor},${tariffFraction}`,
    timezone: settings.timezone,
  }
}

export function validateSettings(values: SettingsValues): SettingsErrors {
  const errors: SettingsErrors = {}
  const count = integer(values.panelCount)
  const wattage = integer(values.panelWattage)
  if (count === undefined || count < 1) errors.panelCount = 'Informe uma quantidade inteira positiva de painéis.'
  if (wattage === undefined || wattage < 1) errors.panelWattage = 'Informe uma potência inteira positiva por painel.'
  if (count && wattage && count * wattage > 12_000) errors.panelCount = 'A potência instalada não pode superar 12 kW.'
  if (values.activeMPPT.length === 0) errors.activeMPPT = 'Mantenha ao menos uma entrada PV ativa.'
  if (!validIPv4(values.loggerHost)) errors.loggerHost = 'Use um endereço IPv4 válido; 192.0.2.1 é apenas um exemplo de formato.'
  if (!/^\d+$/.test(values.loggerSerial) || BigInt(values.loggerSerial) > 4_294_967_295n) errors.loggerSerial = 'Informe um número de série decimal uint32 válido.'
  const port = integer(values.loggerPort)
  if (port === undefined || port < 1 || port > 65_535) errors.loggerPort = 'Informe uma porta inteira entre 1 e 65535.'
  const slave = integer(values.modbusSlave)
  if (slave === undefined || slave < 1 || slave > 247) errors.modbusSlave = 'Informe um endereço Modbus inteiro entre 1 e 247.'
  const latitude = decimal(values.latitude)
  const longitude = decimal(values.longitude)
  if (latitude === undefined || latitude < -90 || latitude > 90) errors.latitude = 'Informe uma latitude finita entre -90 e 90.'
  if (longitude === undefined || longitude < -180 || longitude > 180) errors.longitude = 'Informe uma longitude finita entre -180 e 180.'
  if (!validTimezone(values.timezone)) errors.timezone = 'Use um fuso IANA real, como America/Sao_Paulo.'
  if (!HELIO_ISO_4217_SET.has(values.currency.trim().toUpperCase())) errors.currency = 'Use uma moeda aceita pelo Helio, como BRL.'
  if (tariffMinor(values.tariff) === undefined) errors.tariff = 'Use uma tarifa não negativa com no máximo duas casas decimais.'
  const retention = integer(values.retentionDays)
  if (retention === undefined || retention < 30 || retention > 3650) errors.retentionDays = 'Escolha um número inteiro entre 30 e 3650 dias.'
  return errors
}

export function valuesToSettings(values: SettingsValues): Settings {
  return {
    activeMPPT: [...values.activeMPPT].sort(), currency: values.currency.trim().toUpperCase(),
    latitude: decimal(values.latitude) ?? 0, loggerHost: values.loggerHost.trim(),
    loggerPort: integer(values.loggerPort) ?? 0, loggerSerial: values.loggerSerial,
    longitude: decimal(values.longitude) ?? 0, modbusSlave: integer(values.modbusSlave) ?? 0,
    panelCount: integer(values.panelCount) ?? 0, panelWattage: integer(values.panelWattage) ?? 0,
    retentionDays: integer(values.retentionDays) ?? 0, tariffMinorPerKWh: tariffMinor(values.tariff) ?? 0,
    timezone: values.timezone.trim(),
  }
}

export function derivedInstalledPower(values: SettingsValues) {
  const count = integer(values.panelCount)
  const wattage = integer(values.panelWattage)
  return count && wattage && Number.isSafeInteger(count * wattage) ? count * wattage : 0
}

export function loggerIdentityChanged(values: SettingsValues, original: Settings) {
  return values.loggerHost.trim() !== original.loggerHost
    || values.loggerSerial !== original.loggerSerial
    || integer(values.loggerPort) !== original.loggerPort
    || integer(values.modbusSlave) !== original.modbusSlave
}

export function sameSettings(left: Settings, right: Settings) {
  return JSON.stringify(settingsToValues(left)) === JSON.stringify(settingsToValues(right))
}

export function sameSettingsValues(left: SettingsValues, right: SettingsValues) {
  return JSON.stringify(left) === JSON.stringify(right)
}

export function settingsServerError(message: string, code: string): [keyof SettingsErrors, string] {
  const [field, safe] = serverFieldError(message, code)
  if (field === 'general' || field === 'username' || field === 'password' || field === 'confirmPassword') {
    return ['general', 'O servidor recusou as configurações. Revise os campos e tente novamente.']
  }
  return [field, safe]
}

function validIPv4(value: string) {
  const parts = value.split('.')
  return parts.length === 4 && parts.every((part) => /^(?:0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
}

function validTimezone(value: string) {
  if (!value || value === 'Local') return false
  try { new Intl.DateTimeFormat('en-US', { timeZone: value }).format(); return true } catch { return false }
}
