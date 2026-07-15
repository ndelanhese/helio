import type { LiveEvent } from './types'

export const LIVE_EVENTS_URL = '/api/v1/live/events'

export function parseLiveEvent(value: string): LiveEvent | null {
  try {
    const event = JSON.parse(value) as Partial<LiveEvent>
    if (event.version !== 1 || (event.type !== 'snapshot' && event.type !== 'state') || !event.data) return null
    return event as LiveEvent
  } catch {
    return null
  }
}
