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

export interface MinuteHistoryPointDTO {
  at: string
  powerW: number
}

export interface AggregateHistoryPointDTO {
  at: string
  coveragePct: number
  energyWh: number
  peakPowerW: number
  productiveMinutes?: number
}

export interface HistoryResponse {
  from: string
  points: Array<MinuteHistoryPointDTO | AggregateHistoryPointDTO>
  resolution: 'minute' | 'hour' | 'day' | 'month'
  to: string
}

export type LiveEvent =
  | { kind: 'state'; state: LiveState }
  | { kind: 'snapshot'; snapshot: LiveSnapshot; state: LiveState }

export type Confidence = 'low' | 'medium' | 'high'
export type TrendDirection = 'up' | 'down' | 'stable' | 'insufficient'
export type AlertState = 'open' | 'resolved'
export type AlertKind = 'logger_offline' | 'telemetry_stale' | 'inverter_fault' | 'zero_sunny_generation' | 'persistent_underproduction' | 'grid_voltage' | 'grid_frequency'

export interface EvidenceDTO {
  code: string
  label: string
  unit: string
  value: number
}

export interface TrendDTO {
  changePct: number
  direction: TrendDirection
  windowDays: number
}

export interface InsightsResponse {
  actualWh: number
  confidence: Confidence
  day: string
  evidence: EvidenceDTO[]
  expectedWh: number
  generatedEnergyValue: { currency: string; estimate: true; label: string; minor: number }
  observationWindow: { minimumDays: number; qualifyingDays: number }
  qualifying: boolean
  ratio: number
  trends: { peakPower: TrendDTO; productiveMinutes: TrendDTO }
  version: 'v1'
}

export interface AlertDTO {
  evidence: Array<Omit<EvidenceDTO, 'code'>>
  kind: AlertKind
  openedAt: string
  resolvedAt: string | null
  severity: 'warning' | 'critical'
  state: AlertState
  summary: string
  title: string
}

export interface AlertsResponse {
  alerts: AlertDTO[]
  state: AlertState
  version: 'v1'
}

export interface ComponentHealth {
  collector: string
  database: string
  logger: string
  weather: 'available' | 'stale' | 'unavailable'
  weatherFetchedAt?: string
  weatherUpdatedAt?: string
}
