import { describe, expect, it } from 'vitest'
import { makeServer } from '@/features/servers/fixtures'
import { makeVolume } from './fixtures'
import { attachableVolumes, formatSize, serverChoices } from './serverPicker'

describe('serverChoices', () => {
  it('greys out servers in another location', () => {
    const here = makeServer({ id: 1 })
    const there = makeServer({ id: 2, location: { id: 2, name: 'away', description: '' } })
    const out = serverChoices([here, there], 1)
    expect(out.map((c) => c.disabledReason)).toEqual([null, 'In a different location'])
  })
  it('greys out a full or busy server', () => {
    const full = makeServer({ id: 1, volumes: Array.from({ length: 14 }, (_, i) => i + 1) })
    const busy = makeServer({ id: 2, status: 'rebuilding' })
    const out = serverChoices([full, busy], 1)
    expect(out[0]!.disabledReason).toBe('Already has 14 volumes')
    expect(out[1]!.disabledReason).toBe('Busy right now')
  })
})

describe('attachableVolumes', () => {
  it('keeps detached volumes in the server location', () => {
    const vols = [
      makeVolume({ id: 1 }),
      makeVolume({ id: 2, status: 'attached', server: 9 }),
      makeVolume({ id: 3, location: { id: 2, name: 'away', description: '' } }),
    ]
    expect(attachableVolumes(vols, makeServer()).map((v) => v.id)).toEqual([1])
  })
})

it('formats sizes in GB', () => {
  expect(formatSize(20)).toBe('20 GB')
})
