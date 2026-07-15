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
}

export interface LiveTelemetry {
  [field: string]: unknown
  capturedAt: string
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
  | { type: 'snapshot'; version: 1; data: LiveTelemetry }
  | { type: 'state'; version: 1; data: Record<string, unknown> }
