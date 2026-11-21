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
