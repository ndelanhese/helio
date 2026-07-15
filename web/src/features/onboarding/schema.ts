import type { BootstrapPayload } from '../../api/types'
import { HELIO_ISO_4217_SET } from './currencies'

export interface OnboardingValues {
  activeMPPT: number[]
  confirmPassword: string
  currency: string
  latitude: string
  loggerHost: string
  loggerPort: string
  loggerSerial: string
  longitude: string
  modbusSlave: string
  panelCount: string
  panelWattage: string
  password: string
  retentionDays: string
  tariff: string
  timezone: string
  username: string
}

export type OnboardingField = keyof OnboardingValues
export type FieldErrors = Partial<Record<OnboardingField | 'general', string>>

export const initialOnboardingValues: OnboardingValues = {
  activeMPPT: [1],
  confirmPassword: '',
  currency: 'BRL',
  latitude: '-23.55',
  loggerHost: '',
  loggerPort: '8899',
  loggerSerial: '',
  longitude: '-46.63',
  modbusSlave: '1',
  panelCount: '7',
  panelWattage: '610',
  password: '',
  retentionDays: '730',
  tariff: '0,95',
  timezone: 'America/Sao_Paulo',
  username: '',
}

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

function requiredInteger(value: string, field: OnboardingField) {
  const parsed = integer(value)
  if (parsed === undefined) throw new Error(`invalid integer field: ${field}`)
  return parsed
}

function requiredDecimal(value: string, field: OnboardingField) {
  const parsed = decimal(value)
  if (parsed === undefined) throw new Error(`invalid decimal field: ${field}`)
  return parsed
}

function tariffMinor(value: string) {
  const match = /^(\d+)(?:[.,](\d{1,2}))?$/.exec(value)
  if (!match) return undefined
  const fraction = (match[2] ?? '').padEnd(2, '0')
  const minor = BigInt(match[1]) * 100n + BigInt(fraction || '0')
  if (minor > 1_000_000_000n) return undefined
  return Number(minor)
}

function requiredTariffMinor(value: string) {
  const parsed = tariffMinor(value)
  if (parsed === undefined) throw new Error('invalid tariff field')
  return parsed
}

export function validateStep(step: number, values: OnboardingValues): FieldErrors {
  const errors: FieldErrors = {}
  if (step === 0) {
    if (!values.username.trim()) errors.username = 'Informe o usuário administrador.'
    const passwordLength = [...values.password].length
    const passwordBytes = new TextEncoder().encode(values.password).length
    if (passwordLength < 12) errors.password = 'Use pelo menos 12 caracteres Unicode.'
    else if (passwordBytes > 128) errors.password = 'Use no máximo 128 bytes.'
    if (values.password !== values.confirmPassword) errors.confirmPassword = 'As senhas precisam ser iguais.'
  }
  if (step === 1) {
    if (!validIPv4(values.loggerHost)) errors.loggerHost = 'Use um endereço IPv4 válido; 192.0.2.1 é apenas um exemplo de formato.'
    if (!validLoggerSerial(values.loggerSerial)) errors.loggerSerial = 'Informe um número de série decimal uint32 (até 4294967295).'
    const port = integer(values.loggerPort)
    if (port === undefined || port < 1 || port > 65535) errors.loggerPort = 'Informe uma porta inteira entre 1 e 65535.'
    const slave = integer(values.modbusSlave)
    if (slave === undefined || slave < 1 || slave > 247) errors.modbusSlave = 'Informe um endereço Modbus inteiro entre 1 e 247.'
  }
  if (step === 2) {
    const count = integer(values.panelCount)
    const watts = integer(values.panelWattage)
    if (count === undefined || count < 1) errors.panelCount = 'Informe uma quantidade inteira positiva de painéis.'
    if (watts === undefined || watts < 1) errors.panelWattage = 'Informe uma potência inteira positiva por painel.'
    if (count !== undefined && watts !== undefined && count > 0 && watts > 0 && count * watts > 12000) errors.panelCount = 'A potência instalada não pode superar 12 kW.'
    if (values.activeMPPT.length === 0) errors.activeMPPT = 'Mantenha ao menos uma entrada PV ativa.'
  }
  if (step === 3) {
    const latitude = decimal(values.latitude)
    const longitude = decimal(values.longitude)
    if (latitude === undefined || latitude < -90 || latitude > 90) errors.latitude = 'Informe uma latitude finita entre -90 e 90.'
    if (longitude === undefined || longitude < -180 || longitude > 180) errors.longitude = 'Informe uma longitude finita entre -180 e 180.'
    if (!validTimezone(values.timezone)) errors.timezone = 'Use um fuso IANA real, como America/Sao_Paulo.'
    if (!validCurrency(values.currency)) errors.currency = 'Use um código de moeda ISO 4217, como BRL.'
    if (tariffMinor(values.tariff) === undefined) errors.tariff = 'Use uma tarifa não negativa com no máximo duas casas decimais.'
    const retention = integer(values.retentionDays)
    if (retention === undefined || retention < 30 || retention > 3650) errors.retentionDays = 'Escolha um número inteiro entre 30 e 3650 dias.'
  }
  return errors
}

export function toBootstrapPayload(values: OnboardingValues): BootstrapPayload {
  return {
    username: values.username.trim(),
    password: values.password,
    settings: {
      activeMPPT: [...values.activeMPPT].sort(),
      currency: values.currency.trim().toUpperCase(),
      latitude: requiredDecimal(values.latitude, 'latitude'),
      loggerHost: values.loggerHost.trim(),
      loggerPort: requiredInteger(values.loggerPort, 'loggerPort'),
      loggerSerial: values.loggerSerial,
      longitude: requiredDecimal(values.longitude, 'longitude'),
      modbusSlave: requiredInteger(values.modbusSlave, 'modbusSlave'),
      panelCount: requiredInteger(values.panelCount, 'panelCount'),
      panelWattage: requiredInteger(values.panelWattage, 'panelWattage'),
      retentionDays: requiredInteger(values.retentionDays, 'retentionDays'),
      tariffMinorPerKWh: requiredTariffMinor(values.tariff),
      timezone: values.timezone.trim(),
    },
  }
}

export function installedPower(values: OnboardingValues) {
  const count = integer(values.panelCount)
  const perPanel = integer(values.panelWattage)
  if (count === undefined || perPanel === undefined) return 0
  const watts = count * perPanel
  return Number.isSafeInteger(watts) ? watts : 0
}

export function serverFieldError(message: string, code = 'invalid_settings'): [OnboardingField | 'general', string] {
  const normalized = message.toLowerCase()
  if (code === 'invalid_request') return ['username', 'Informe o usuário administrador.']
  if (code === 'invalid_password' || normalized.includes('password')) return ['password', 'A senha precisa ter ao menos 12 caracteres e no máximo 128 bytes.']
  const mappings: Array<[RegExp, OnboardingField, string]> = [
    [/username/, 'username', 'Informe o usuário administrador.'],
    [/logger host|loggerhost/, 'loggerHost', 'O servidor recusou o endereço IPv4 do logger.'],
    [/logger serial|loggerserial|serial/, 'loggerSerial', 'Informe um número de série decimal uint32 válido.'],
    [/logger port|loggerport/, 'loggerPort', 'Informe uma porta entre 1 e 65535.'],
    [/modbus slave|modbusslave/, 'modbusSlave', 'Informe um endereço Modbus entre 1 e 247.'],
    [/installed power|panel count/, 'panelCount', 'Revise a quantidade e a potência total instalada.'],
    [/panel wattage|panelwattage|wattage/, 'panelWattage', 'Revise a potência por painel.'],
    [/mppt/, 'activeMPPT', 'Mantenha ao menos uma entrada PV válida ativa.'],
    [/latitude/, 'latitude', 'Informe uma latitude entre -90 e 90.'],
    [/longitude/, 'longitude', 'Informe uma longitude entre -180 e 180.'],
    [/timezone|time zone/, 'timezone', 'O servidor não reconheceu esse fuso IANA.'],
    [/currency/, 'currency', 'O servidor não reconheceu essa moeda ISO 4217.'],
    [/tariff/, 'tariff', 'O servidor recusou essa tarifa.'],
    [/retention/, 'retentionDays', 'Escolha uma retenção entre 30 e 3650 dias.'],
  ]
  for (const [pattern, field, safeMessage] of mappings) {
    if (pattern.test(normalized)) return [field, safeMessage]
  }
  return ['general', 'Não foi possível criar o Helio. Revise os dados e tente novamente.']
}

function validIPv4(value: string) {
  const parts = value.split('.')
  return parts.length === 4 && parts.every((part) => /^(?:0|[1-9]\d{0,2})$/.test(part) && Number(part) <= 255)
}

function validLoggerSerial(value: string) {
  if (!/^\d+$/.test(value)) return false
  try { return BigInt(value) <= 4_294_967_295n } catch { return false }
}

function validTimezone(value: string) {
  if (!value || value === 'Local') return false
  try {
    new Intl.DateTimeFormat('en-US', { timeZone: value }).format()
    return true
  } catch {
    return false
  }
}

function validCurrency(value: string) {
  const normalized = value.trim().toUpperCase()
  return /^[A-Z]{3}$/.test(normalized) && HELIO_ISO_4217_SET.has(normalized)
}
