import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'

import { HELIO_ISO_4217 } from './currencies'

describe('HELIO_ISO_4217', () => {
  it('matches the authoritative backend validator exactly', () => {
    const backend = readFileSync('../internal/config/validation.go', 'utf8')
    const match = /const iso4217 = " ([A-Z ]+) "/.exec(backend)
    if (!match) throw new Error('backend ISO 4217 set not found')

    expect(HELIO_ISO_4217).toEqual(match[1].split(' '))
  })
})
