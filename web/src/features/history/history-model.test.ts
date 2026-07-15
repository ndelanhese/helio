import { describe, expect, it } from 'vitest'

import {
  buildChartRows,
  buildHistoryView,
  getPeriodRange,
  parseHistorySearch,
  toChartSegments,
  type AggregateHistoryPoint,
  type MinuteHistoryPoint,
} from './history-model'

describe('history model', () => {
  const anchor = new Date('2026-07-14T15:30:00Z')

  it.each([
    ['day', 'minute', '2026-07-14T03:00:00.000Z', '2026-07-15T03:00:00.000Z'],
    ['week', 'hour', '2026-07-13T03:00:00.000Z', '2026-07-20T03:00:00.000Z'],
    ['month', 'day', '2026-07-01T03:00:00.000Z', '2026-08-01T03:00:00.000Z'],
    ['year', 'month', '2026-01-01T03:00:00.000Z', '2027-01-01T03:00:00.000Z'],
  ] as const)('creates a local %s range using the server resolution %s', (period, resolution, from, to) => {
    expect(getPeriodRange(period, anchor, 'America/Sao_Paulo')).toMatchObject({ period, resolution, from, to })
  })

  it('keeps previous calendar periods aligned in the configured timezone', () => {
    const range = getPeriodRange('month', new Date('2026-03-20T12:00:00Z'), 'America/Sao_Paulo')
    expect(range.previousFrom).toBe('2026-02-01T03:00:00.000Z')
    expect(range.previousTo).toBe('2026-03-01T03:00:00.000Z')
  })

  it('round-trips a valid URL range and rejects server-invalid minute windows', () => {
    const valid = new URLSearchParams({
      period: 'day',
      from: '2026-07-14T03:00:00.000Z',
      to: '2026-07-15T03:00:00.000Z',
    })
    expect(parseHistorySearch(valid, anchor, 'America/Sao_Paulo')).toMatchObject({ period: 'day', resolution: 'minute' })

    const invalid = new URLSearchParams({
      period: 'day',
      from: '2025-01-01T00:00:00.000Z',
      to: '2026-07-15T00:00:00.000Z',
    })
    const fallback = parseHistorySearch(invalid, anchor, 'America/Sao_Paulo')
    expect(fallback.from).toBe('2026-07-14T03:00:00.000Z')
  })

  it('splits gaps over 90 seconds without inserting a synthetic zero', () => {
    const points: MinuteHistoryPoint[] = [
      { at: '2026-07-14T12:00:00Z', powerW: 800 },
      { at: '2026-07-14T12:01:00Z', powerW: 900 },
      { at: '2026-07-14T12:04:00Z', powerW: 1000 },
    ]
    expect(toChartSegments(points, 90_000)).toEqual([
      points.slice(0, 2),
      points.slice(2),
    ])
    expect(toChartSegments(points, 90_000).flat().some((point) => point.powerW === 0)).toBe(false)
  })

  it('marks a gap midpoint for a Sem dados tooltip without a power value', () => {
    const current = buildHistoryView([
      { at: '2026-07-14T12:00:00Z', powerW: 800 },
      { at: '2026-07-14T12:04:00Z', powerW: 1000 },
    ], 'minute')
    const range = getPeriodRange('day', anchor, 'America/Sao_Paulo')
    const gap = buildChartRows(current, undefined, range).find((row) => row.gapLabel)
    expect(gap).toMatchObject({ gapLabel: 'Sem dados' })
    expect(Object.values(gap ?? {}).includes(0)).toBe(false)
  })

  it('derives minute energy, peak, productive duration and explicit gaps only from observed intervals', () => {
    const points: MinuteHistoryPoint[] = [
      { at: '2026-07-14T12:00:00Z', powerW: 600 },
      { at: '2026-07-14T12:01:00Z', powerW: 1200 },
      { at: '2026-07-14T12:04:00Z', powerW: 2400 },
    ]
    const view = buildHistoryView(points, 'minute')
    expect(view.summary).toMatchObject({ energyWh: 15, peakPowerW: 2400, productiveMinutes: 1, coveragePct: null })
    expect(view.gaps).toEqual([{ from: '2026-07-14T12:01:00Z', to: '2026-07-14T12:04:00Z' }])
  })

  it('summarizes persisted aggregates and flags coverage below 95 percent', () => {
    const points: AggregateHistoryPoint[] = [
      { at: '2026-07-14T03:00:00Z', energyWh: 2500, peakPowerW: 3200, productiveMinutes: 240, coveragePct: 92 },
      { at: '2026-07-15T03:00:00Z', energyWh: 3000, peakPowerW: 4100, productiveMinutes: 300, coveragePct: 96 },
    ]
    const view = buildHistoryView(points, 'day')
    expect(view.summary).toEqual({ energyWh: 5500, peakPowerW: 4100, productiveMinutes: 540, coveragePct: 94 })
    expect(view.hasLowCoverage).toBe(true)
  })
})
