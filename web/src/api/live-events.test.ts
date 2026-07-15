import { describe, expect, it } from 'vitest'

import { parseLiveEvent } from './live-events'

describe('parseLiveEvent', () => {
  it('accepts only known versioned live events', () => {
    expect(parseLiveEvent('{"version":1,"type":"state","data":{"connection":"online"}}')).toEqual({
      version: 1,
      type: 'state',
      data: { connection: 'online' },
    })
    expect(parseLiveEvent('{"version":2,"type":"state","data":{}}')).toBeNull()
    expect(parseLiveEvent('not-json')).toBeNull()
  })
})
