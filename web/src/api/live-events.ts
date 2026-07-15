import type { LiveEvent, LiveSnapshot, LiveState, MPPTTelemetry } from './types'

export const LIVE_EVENTS_URL = '/api/v1/live/events'

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null
}

function finite(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

function isMPPT(value: unknown): value is MPPTTelemetry {
  return isRecord(value) && typeof value.active === 'boolean'
    && finite(value.currentA) && finite(value.powerW) && finite(value.voltageV)
}

function isSnapshot(value: unknown): value is LiveSnapshot {
  if (!isRecord(value) || !isRecord(value.grid)) return false
  return typeof value.observedAt === 'string' && Number.isFinite(Date.parse(value.observedAt))
    && typeof value.status === 'string' && finite(value.acPowerW)
    && finite(value.energyTodayWh) && finite(value.energyLifetimeWh)
    && isMPPT(value.pv1) && isMPPT(value.pv2)
    && finite(value.grid.voltageV) && finite(value.grid.frequencyHz)
    && Array.isArray(value.faultCodes)
    && value.faultCodes.every((code) => Number.isInteger(code) && code >= 0)
}

function optionalString(value: Record<string, unknown>, key: string) {
  return value[key] === undefined || typeof value[key] === 'string'
}

function isState(value: unknown): value is LiveState {
  if (!isRecord(value) || 'version' in value || typeof value.stale !== 'boolean') return false
  return optionalString(value, 'lastSuccess') && optionalString(value, 'lastError')
    && optionalString(value, 'lastErrorAt') && optionalString(value, 'errorClass')
    && (value.lastSuccess === undefined || Number.isFinite(Date.parse(value.lastSuccess as string)))
    && (value.lastErrorAt === undefined || Number.isFinite(Date.parse(value.lastErrorAt as string)))
    && (value.snapshot === undefined || isSnapshot(value.snapshot))
}

export function parseLiveEvent(kind: string, value: string): LiveEvent | null {
  try {
    const parsed: unknown = JSON.parse(value)
    if (kind === 'state') return isState(parsed) ? { kind: 'state', state: parsed } : null
    if (kind !== 'snapshot' || !isRecord(parsed) || parsed.kind !== 'snapshot' || 'version' in parsed) return null
    if (!isState(parsed.state) || !isSnapshot(parsed.snapshot)) return null
    return { kind: 'snapshot', state: parsed.state, snapshot: parsed.snapshot }
  } catch {
    return null
  }
}
