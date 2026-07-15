export type HistoryPeriod = 'day' | 'week' | 'month' | 'year'
export type HistoryResolution = 'minute' | 'hour' | 'day' | 'month'

export interface MinuteHistoryPoint {
  at: string
  powerW: number
}

export interface AggregateHistoryPoint {
  at: string
  coveragePct: number
  energyWh: number
  peakPowerW: number
  productiveMinutes?: number
}

export type HistoryPoint = MinuteHistoryPoint | AggregateHistoryPoint

export interface HistoryRange {
  from: string
  period: HistoryPeriod
  previousFrom: string
  previousTo: string
  resolution: HistoryResolution
  to: string
}

export interface ChartPoint {
  at: string
  powerW: number
}

export interface HistorySummary {
  coveragePct: number | null
  energyWh: number
  peakPowerW: number
  productiveMinutes: number
}

export interface HistoryView {
  chartPoints: ChartPoint[]
  gaps: Array<{ from: string; to: string }>
  hasLowCoverage: boolean
  segments: ChartPoint[][]
  summary: HistorySummary
}

export interface ChartRow {
  at: number
  gapLabel?: 'Sem dados'
  [key: string]: number | string | undefined
}

const PERIOD_RESOLUTION: Record<HistoryPeriod, HistoryResolution> = {
  day: 'minute',
  week: 'hour',
  month: 'day',
  year: 'month',
}

const RESOLUTION_GAP_MS: Record<HistoryResolution, number> = {
  minute: 90_000,
  hour: 90 * 60_000,
  day: 36 * 60 * 60_000,
  month: 46 * 24 * 60 * 60_000,
}

interface CalendarParts { day: number; hour: number; minute: number; month: number; second: number; year: number }

function calendarParts(date: Date, timezone: string): CalendarParts {
  const values = Object.fromEntries(new Intl.DateTimeFormat('en-CA', {
    day: '2-digit', hour: '2-digit', hourCycle: 'h23', minute: '2-digit', month: '2-digit', second: '2-digit',
    timeZone: timezone, year: 'numeric',
  }).formatToParts(date).filter((part) => part.type !== 'literal').map((part) => [part.type, Number(part.value)]))
  return values as unknown as CalendarParts
}

function normalizeCalendar(parts: CalendarParts) {
  const date = new Date(Date.UTC(parts.year, parts.month - 1, parts.day, parts.hour, parts.minute, parts.second))
  return { day: date.getUTCDate(), hour: date.getUTCHours(), minute: date.getUTCMinutes(), month: date.getUTCMonth() + 1, second: date.getUTCSeconds(), year: date.getUTCFullYear() }
}

function zonedCalendarToDate(parts: CalendarParts, timezone: string) {
  const normalized = normalizeCalendar(parts)
  const desired = Date.UTC(normalized.year, normalized.month - 1, normalized.day, normalized.hour, normalized.minute, normalized.second)
  let candidate = desired
  for (let pass = 0; pass < 3; pass += 1) {
    const actual = calendarParts(new Date(candidate), timezone)
    const actualAsUTC = Date.UTC(actual.year, actual.month - 1, actual.day, actual.hour, actual.minute, actual.second)
    candidate += desired - actualAsUTC
  }
  return new Date(candidate)
}

function shiftCalendar(parts: CalendarParts, period: HistoryPeriod, amount: number): CalendarParts {
  const date = new Date(Date.UTC(parts.year, parts.month - 1, parts.day))
  if (period === 'day') date.setUTCDate(date.getUTCDate() + amount)
  if (period === 'week') date.setUTCDate(date.getUTCDate() + 7 * amount)
  if (period === 'month') date.setUTCMonth(date.getUTCMonth() + amount)
  if (period === 'year') date.setUTCFullYear(date.getUTCFullYear() + amount)
  return { day: date.getUTCDate(), hour: 0, minute: 0, month: date.getUTCMonth() + 1, second: 0, year: date.getUTCFullYear() }
}

function periodStart(period: HistoryPeriod, anchor: Date, timezone: string): CalendarParts {
  const local = calendarParts(anchor, timezone)
  const start = { ...local, hour: 0, minute: 0, second: 0 }
  if (period === 'week') {
    const weekday = new Date(Date.UTC(local.year, local.month - 1, local.day)).getUTCDay()
    return shiftCalendar(start, 'day', -((weekday + 6) % 7))
  }
  if (period === 'month') start.day = 1
  if (period === 'year') { start.day = 1; start.month = 1 }
  return start
}

function iso(date: Date) { return date.toISOString() }

export function getPeriodRange(period: HistoryPeriod, anchor: Date, timezone: string): HistoryRange {
  const start = periodStart(period, anchor, timezone)
  const end = shiftCalendar(start, period, 1)
  const previous = shiftCalendar(start, period, -1)
  const from = iso(zonedCalendarToDate(start, timezone))
  return {
    from,
    period,
    previousFrom: iso(zonedCalendarToDate(previous, timezone)),
    previousTo: from,
    resolution: PERIOD_RESOLUTION[period],
    to: iso(zonedCalendarToDate(end, timezone)),
  }
}

function isPeriod(value: string | null): value is HistoryPeriod {
  return value === 'day' || value === 'week' || value === 'month' || value === 'year'
}

export function parseHistorySearch(search: URLSearchParams, anchor: Date, timezone: string): HistoryRange {
  const period = isPeriod(search.get('period')) ? search.get('period') as HistoryPeriod : 'day'
  const fallback = getPeriodRange(period, anchor, timezone)
  const from = search.get('from')
  const to = search.get('to')
  if (!from || !to) return fallback
  const fromMs = Date.parse(from)
  const toMs = Date.parse(to)
  if (!Number.isFinite(fromMs) || !Number.isFinite(toMs) || fromMs >= toMs) return fallback
  if (PERIOD_RESOLUTION[period] === 'minute' && toMs - fromMs > 366 * 24 * 60 * 60_000) return fallback
  const start = periodStart(period, new Date(fromMs + 12 * 60 * 60_000), timezone)
  return {
    from: iso(new Date(fromMs)), period,
    previousFrom: iso(zonedCalendarToDate(shiftCalendar(start, period, -1), timezone)),
    previousTo: iso(new Date(fromMs)), resolution: PERIOD_RESOLUTION[period], to: iso(new Date(toMs)),
  }
}

export function serializeHistoryRange(range: HistoryRange) {
  return new URLSearchParams({ period: range.period, from: range.from, to: range.to })
}

export function isMinutePoint(point: HistoryPoint): point is MinuteHistoryPoint {
  return 'powerW' in point
}

export function toChartPoint(point: HistoryPoint): ChartPoint {
  return { at: point.at, powerW: isMinutePoint(point) ? point.powerW : point.peakPowerW }
}

export function toChartSegments<T extends { at: string }>(points: T[], maxGapMs: number): T[][] {
  if (points.length === 0) return []
  const segments: T[][] = [[points[0]]]
  for (let index = 1; index < points.length; index += 1) {
    const previous = points[index - 1]
    const point = points[index]
    if (Date.parse(point.at) - Date.parse(previous.at) > maxGapMs) segments.push([])
    segments.at(-1)?.push(point)
  }
  return segments
}

function summarizeMinute(points: MinuteHistoryPoint[]): HistorySummary {
  let energyWh = 0
  let productiveMinutes = 0
  for (let index = 1; index < points.length; index += 1) {
    const previous = points[index - 1]
    const point = points[index]
    const elapsedMs = Date.parse(point.at) - Date.parse(previous.at)
    if (elapsedMs <= 0 || elapsedMs > RESOLUTION_GAP_MS.minute) continue
    energyWh += ((previous.powerW + point.powerW) / 2) * elapsedMs / 3_600_000
    if (previous.powerW > 0 || point.powerW > 0) productiveMinutes += elapsedMs / 60_000
  }
  return { coveragePct: null, energyWh, peakPowerW: Math.max(0, ...points.map((point) => point.powerW)), productiveMinutes }
}

function summarizeAggregate(points: AggregateHistoryPoint[]): HistorySummary {
  if (points.length === 0) return { coveragePct: null, energyWh: 0, peakPowerW: 0, productiveMinutes: 0 }
  return {
    coveragePct: points.reduce((total, point) => total + point.coveragePct, 0) / points.length,
    energyWh: points.reduce((total, point) => total + point.energyWh, 0),
    peakPowerW: Math.max(0, ...points.map((point) => point.peakPowerW)),
    productiveMinutes: points.reduce((total, point) => total + (point.productiveMinutes ?? 0), 0),
  }
}

export function buildHistoryView(points: HistoryPoint[], resolution: HistoryResolution): HistoryView {
  const ordered = [...points].sort((left, right) => Date.parse(left.at) - Date.parse(right.at))
  const chartPoints = ordered.map(toChartPoint)
  const segments = toChartSegments(chartPoints, RESOLUTION_GAP_MS[resolution])
  const gaps = segments.slice(0, -1).map((segment, index) => ({ from: segment.at(-1)?.at ?? '', to: segments[index + 1][0]?.at ?? '' }))
  const summary = ordered.every(isMinutePoint)
    ? summarizeMinute(ordered)
    : summarizeAggregate(ordered.filter((point): point is AggregateHistoryPoint => !isMinutePoint(point)))
  return { chartPoints, gaps, hasLowCoverage: summary.coveragePct !== null && summary.coveragePct < 95, segments, summary }
}

export function buildChartRows(current: HistoryView, previous: HistoryView | undefined, range: HistoryRange) {
  const rows = new Map<number, ChartRow>()
  const add = (at: number, key: string, powerW: number, sourceAt: string) => {
    const row = rows.get(at) ?? { at }
    row[key] = powerW
    row[`${key}At`] = sourceAt
    rows.set(at, row)
  }
  current.segments.forEach((segment, segmentIndex) => {
    segment.forEach((point) => { add(Date.parse(point.at), `current${segmentIndex}`, point.powerW, point.at) })
  })
  current.gaps.forEach((gap) => {
    const at = (Date.parse(gap.from) + Date.parse(gap.to)) / 2
    rows.set(at, { ...(rows.get(at) ?? { at }), gapLabel: 'Sem dados' })
  })
  if (previous) {
    const currentStart = Date.parse(range.from)
    const currentDuration = Date.parse(range.to) - currentStart
    const previousStart = Date.parse(range.previousFrom)
    const previousDuration = Date.parse(range.previousTo) - previousStart
    previous.segments.forEach((segment, segmentIndex) => {
      segment.forEach((point) => {
        const progress = (Date.parse(point.at) - previousStart) / previousDuration
        add(currentStart + progress * currentDuration, `previous${segmentIndex}`, point.powerW, point.at)
      })
    })
  }
  return [...rows.values()].sort((left, right) => left.at - right.at)
}
