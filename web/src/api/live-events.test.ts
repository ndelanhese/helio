import { describe, expect, it } from 'vitest'

import { parseLiveEvent } from './live-events'

describe('parseLiveEvent', () => {
  const snapshot = {
    observedAt: '2026-07-14T15:42:00Z', status: 'normal', acPowerW: 2070,
    energyTodayWh: 12340, energyLifetimeWh: 4567800,
    pv1: { active: true, voltageV: 267.1, currentA: 8, powerW: 2070 },
    pv2: { active: false, voltageV: 0, currentA: 0, powerW: 0 },
    grid: { voltageV: 267.1, frequencyHz: 59.97 }, faultCodes: [],
  }
  const state = { lastSuccess: snapshot.observedAt, stale: false, snapshot }

  it('parses the exact backend state and snapshot payloads', () => {
    expect(parseLiveEvent('state', JSON.stringify(state))).toEqual({ kind: 'state', state })
    expect(parseLiveEvent('snapshot', JSON.stringify({ kind: 'snapshot', state, snapshot }))).toEqual({ kind: 'snapshot', state, snapshot })
  })

  it('rejects unknown, versioned, incomplete, non-finite, and malformed payloads', () => {
    expect(parseLiveEvent('unknown', JSON.stringify(state))).toBeNull()
    expect(parseLiveEvent('state', JSON.stringify({ ...state, version: 2 }))).toBeNull()
    expect(parseLiveEvent('snapshot', JSON.stringify({ kind: 'snapshot', state, snapshot: { ...snapshot, pv1: { active: true, powerW: 1 } } }))).toBeNull()
    expect(parseLiveEvent('state', JSON.stringify({ ...state, snapshot: { ...snapshot, grid: { ...snapshot.grid, frequencyHz: '59.97' } } }))).toBeNull()
    expect(parseLiveEvent('state', JSON.stringify({ ...state, snapshot: { ...snapshot, faultCodes: [1, -2, 3.5] } }))).toBeNull()
    expect(parseLiveEvent('state', 'not-json')).toBeNull()
  })
})
