import type { BootstrapPayload } from '../../api/types'

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

const integer = (value: string) => /^\d+$/.test(value) ? Number(value) : Number.NaN
const decimal = (value: string) => Number(value.replace(',', '.'))

export function validateStep(step: number, values: OnboardingValues): FieldErrors {
  const errors: FieldErrors = {}
  if (step === 0) {
    if (!values.username.trim()) errors.username = 'Informe o usuário administrador.'
    const passwordBytes = new TextEncoder().encode(values.password).length
    if (passwordBytes < 12) errors.password = 'Use pelo menos 12 caracteres.'
    else if (passwordBytes > 128) errors.password = 'Use no máximo 128 bytes.'
    if (values.password !== values.confirmPassword) errors.confirmPassword = 'As senhas precisam ser iguais.'
  }
  if (step === 1) {
    if (!validIPv4(values.loggerHost)) errors.loggerHost = 'Use um endereço IPv4, como 192.168.1.50.'
    if (!/^\d{1,10}$/.test(values.loggerSerial)) errors.loggerSerial = 'O número de série precisa conter apenas dígitos.'
    const port = integer(values.loggerPort)
    if (port < 1 || port > 65535) errors.loggerPort = 'Informe uma porta entre 1 e 65535.'
    const slave = integer(values.modbusSlave)
    if (slave < 1 || slave > 247) errors.modbusSlave = 'Informe um endereço Modbus entre 1 e 247.'
  }
  if (step === 2) {
    const count = integer(values.panelCount)
    const watts = integer(values.panelWattage)
    if (count < 1) errors.panelCount = 'Informe ao menos um painel.'
    if (watts < 1) errors.panelWattage = 'Informe a potência positiva do painel.'
    if (count > 0 && watts > 0 && count * watts > 12000) errors.panelCount = 'A potência instalada não pode superar 12 kW.'
    if (values.activeMPPT.length === 0) errors.activeMPPT = 'Mantenha ao menos uma entrada PV ativa.'
  }
  if (step === 3) {
    const latitude = decimal(values.latitude)
    const longitude = decimal(values.longitude)
    if (!Number.isFinite(latitude) || latitude < -90 || latitude > 90) errors.latitude = 'Informe uma latitude entre -90 e 90.'
    if (!Number.isFinite(longitude) || longitude < -180 || longitude > 180) errors.longitude = 'Informe uma longitude entre -180 e 180.'
    if (!/^[A-Za-z_+-]+\/[A-Za-z0-9_+/-]+$/.test(values.timezone)) errors.timezone = 'Use um fuso IANA, como America/Sao_Paulo.'
    if (!/^[A-Z]{3}$/.test(values.currency)) errors.currency = 'Use um código de moeda com três letras maiúsculas.'
    if (!Number.isFinite(decimal(values.tariff)) || decimal(values.tariff) < 0) errors.tariff = 'Informe uma tarifa igual ou maior que zero.'
    const retention = integer(values.retentionDays)
    if (retention < 30 || retention > 3650) errors.retentionDays = 'Escolha entre 30 e 3650 dias.'
  }
  return errors
}

export function toBootstrapPayload(values: OnboardingValues): BootstrapPayload {
  return {
    username: values.username.trim(),
    password: values.password,
    settings: {
      activeMPPT: [...values.activeMPPT].sort(),
      currency: values.currency,
      latitude: decimal(values.latitude),
      loggerHost: values.loggerHost.trim(),
      loggerPort: integer(values.loggerPort),
      loggerSerial: values.loggerSerial,
      longitude: decimal(values.longitude),
      modbusSlave: integer(values.modbusSlave),
      panelCount: integer(values.panelCount),
      panelWattage: integer(values.panelWattage),
      retentionDays: integer(values.retentionDays),
      tariffMinorPerKWh: Math.round(decimal(values.tariff) * 100),
      timezone: values.timezone.trim(),
    },
  }
}

export function installedPower(values: OnboardingValues) {
  const watts = integer(values.panelCount) * integer(values.panelWattage)
  return Number.isFinite(watts) ? watts : 0
}

export function serverFieldError(message: string): [OnboardingField | 'general', string] {
  const normalized = message.toLowerCase()
  if (normalized.includes('serial')) return ['loggerSerial', 'O número de série precisa conter apenas dígitos e caber no formato do logger.']
  if (normalized.includes('host')) return ['loggerHost', 'O servidor recusou o endereço do logger. Confira o IPv4 da rede local.']
  if (normalized.includes('password')) return ['password', 'A senha precisa ter entre 12 e 128 bytes.']
  if (normalized.includes('timezone')) return ['timezone', 'O servidor não reconheceu esse fuso IANA.']
  if (normalized.includes('tariff')) return ['tariff', 'O servidor recusou essa tarifa.']
  return ['general', 'Não foi possível criar o Helio. Revise os dados e tente novamente.']
}

function validIPv4(value: string) {
  const parts = value.split('.').map(Number)
  return parts.length === 4 && parts.every((part) => Number.isInteger(part) && part >= 0 && part <= 255)
}
