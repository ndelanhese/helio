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

export interface BillingCycle {
  activeConsumptionKWh: number
  creditBalanceKWh: number
  creditsUsedKWh: number
  id: number
  injectedKWh: number
  readingEnd: string
  readingStart: string
  tariffVersionId: number
  totalPaidMinor: number
	flagChargeMinor: number
}

export interface FinancialProjection {
  billingCycleId: number
  calculatedAt: string
  cipMinor: number
  compensationMinor: number
  consumptionMinor: number
  flagMinor: number
  flagChargeMinor: number
  id: number
  isEstimate: boolean
  tariffVersionId: number
  taxesMinor: number
  totalMinor: number
  withoutSolarCompensationMinor: number
  displayTotal: string
  displayWithoutSolar: string
  displayRows: DisplayRow[]
}

export interface DisplayRow { label: string; value: string }
export interface FinanceSummary { cycles: BillingCycle[]; latestProjection: FinancialProjection | null; creditBalanceKWh: number; nextCreditExpiry: string | null }

export interface TariffProposal {
  approvedAt: string | null
  availabilityKWh: number
  cipMinor: number
  compensationTEMicrosPerKWh: number
  compensationTUSDMicrosPerKWh: number
  consumptionTEMicrosPerKWh: number
  consumptionTUSDMicrosPerKWh: number
  distributor: string
  effectiveFrom: string
  effectiveTo: string
  flagMicrosPerKWh: number
  id: number
  parserVersion: string
  retrievedAt: string
  sourceUrl: string
	displayRates: Array<{ label: string; approved: string; proposal: string; delta: string }>
}

export interface BillingCycleInput {
  activeConsumptionKWh: number
  creditBalanceKWh: number
  creditsUsedKWh: number
  injectedKWh: number
  readingEnd: string
  readingStart: string
  totalPaidMinor: number
  flagChargeMinor: number
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
  coveragePct: number
  current: number
  delta: number
  deltaPct: number
  direction: TrendDirection
  previous: number
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
  limit: number
  state: AlertState
  version: 'v1'
}

export interface ComponentHealth {
  analysis?: string
  alerts?: string
  collector: string
  collectorUpdatedAt?: string
  database: string
  databaseUpdatedAt?: string
  jobs?: string
  logger: string
  loggerUpdatedAt?: string
  weather: 'available' | 'stale' | 'unavailable'
	cloudCoverPct?: number
	precipitationMM?: number
	temperatureC?: number
	weatherCode?: number
	windSpeedKMH?: number
	irradianceWM2?: number
  weatherFetchedAt?: string
  weatherUpdatedAt?: string
}
