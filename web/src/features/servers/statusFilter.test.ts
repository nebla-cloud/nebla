import { describe, expect, it } from 'vitest'
import { makeServer } from './fixtures'
import { filterServers, parseStatusFilter } from './statusFilter'

const list = [
  makeServer({ id: 1, status: 'running' }),
  makeServer({ id: 2, status: 'off' }),
  makeServer({ id: 3, status: 'starting' }),
]

describe('statusFilter', () => {
  it('parses known values and falls back to all', () => {
    expect(parseStatusFilter('off')).toBe('off')
    expect(parseStatusFilter('running')).toBe('running')
    expect(parseStatusFilter('all')).toBe('all')
    expect(parseStatusFilter('bogus')).toBe('all')
    expect(parseStatusFilter(undefined)).toBe('all')
    expect(parseStatusFilter(['off'])).toBe('all')
  })

  it('filters by exact status', () => {
    expect(filterServers(list, 'all')).toHaveLength(3)
    expect(filterServers(list, 'running').map((s) => s.id)).toEqual([1])
    expect(filterServers(list, 'off').map((s) => s.id)).toEqual([2])
  })
})
