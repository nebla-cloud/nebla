import { describe, expect, it } from 'vitest'
import type { ServerType } from '@/api/types'
import { makeServer } from './fixtures'
import { typeChoices } from './resize'

const t = (id: number, cores: number, mem: number, disk: number, locations = [1]): ServerType => ({
  id,
  name: `t${id}`,
  description: '',
  cores,
  memory_mb: mem,
  disk_gb: disk,
  locations,
  deprecated: false,
})
const types = [t(1, 2, 4096, 40), t(2, 4, 8192, 80), t(3, 1, 2048, 20), t(4, 8, 16384, 160, [2])]
const server = makeServer()

describe('typeChoices', () => {
  it('current marked', () => {
    const c = typeChoices(types, server, false)[0]!
    expect(c.current).toBe(true)
    expect(c.disabledReason).toBe('Current type')
  })

  it('smaller disk disabled only when growing', () => {
    expect(typeChoices(types, server, false)[2]!.disabledReason).toBeNull()
    expect(typeChoices(types, server, true)[2]!.disabledReason).toBe(
      "Smaller disk than this server's 40 GB",
    )
  })

  it('delta text', () => {
    expect(typeChoices(types, server, false)[1]!.delta).toBe(
      '+2 vCPU, +4 GB memory, disk stays 40 GB',
    )
    expect(typeChoices(types, server, true)[1]!.delta).toBe(
      '+2 vCPU, +4 GB memory, disk grows to 80 GB',
    )
    expect(typeChoices(types, server, false)[2]!.delta).toBe(
      '-1 vCPU, -2 GB memory, disk stays 40 GB',
    )
  })

  it('location filter', () => {
    expect(typeChoices(types, server, false)[3]!.disabledReason).toBe('Not available in home')
  })
})
