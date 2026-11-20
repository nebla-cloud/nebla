import { describe, expect, it } from 'vitest'
import { formatDate, formatMemory, typeSpec } from './format'

describe('format', () => {
  it('formats memory', () => {
    expect(formatMemory(4096)).toBe('4 GB')
    expect(formatMemory(512)).toBe('512 MB')
    expect(formatMemory(1536)).toBe('1.5 GB')
  })

  it('formats dates in en-GB', () => {
    expect(formatDate('2026-11-18T10:00:00Z')).toBe('18 Nov 2026')
  })

  it('describes a server type', () => {
    expect(
      typeSpec({
        id: 1,
        name: 'small-2',
        description: '',
        cores: 2,
        memory_mb: 4096,
        disk_gb: 40,
        locations: [],
        deprecated: false,
      }),
    ).toBe('2 vCPU, 4 GB, 40 GB')
  })
})
