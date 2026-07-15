export interface ApiErrorEnvelope {
  error: {
    code: string
    message: string
  }
}

export interface BootstrapStatus {
  open: boolean
}

export interface AuthCredentials {
  csrfToken: string
  expiresAt: string
  userId: string
  username: string
}

export interface LoginPayload {
  password: string
  username: string
}

export interface BootstrapPayload extends LoginPayload {
  settings: Omit<Settings, 'installedPowerW'>
}

export interface Session {
  csrfToken: string
  expiresAt: string
  userId: string
  username: string
}

export interface Settings {
  activeMPPT: number[]
  currency: string
  latitude: number
  loggerHost: string
  loggerPort: number
  loggerSerial: string
  longitude: number
  modbusSlave: number
  panelCount: number
  panelWattage: number
  retentionDays: number
  tariffMinorPerKWh: number
  timezone: string
  installedPowerW?: number
}

export interface MPPTTelemetry {
  active: boolean
  currentA: number
  powerW: number
  voltageV: number
}

export interface LiveSnapshot {
  acPowerW: number
  energyLifetimeWh: number
  energyTodayWh: number
  faultCodes: number[]
  grid: {
    frequencyHz: number
    voltageV: number
  }
  observedAt: string
  pv1: MPPTTelemetry
  pv2: MPPTTelemetry
  status: string
}

export interface LiveState {
  errorClass?: string
  lastError?: string
  lastErrorAt?: string
  lastSuccess?: string
  snapshot?: LiveSnapshot
  stale: boolean
}

export interface HistoryPoint {
  timestamp: string
  watts: number | null
}

export interface HistoryResponse {
  [field: string]: unknown
  points: HistoryPoint[]
}

export type LiveEvent =
  | { type: 'snapshot'; version: 1; data: LiveState }
  | { type: 'state'; version: 1; data: Record<string, unknown> }
