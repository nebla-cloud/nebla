// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
