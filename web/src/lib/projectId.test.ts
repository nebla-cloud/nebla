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

import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import { parseProjectId } from './projectId'

describe('projectId', () => {
  it('has no app imports', () => {
    const src = readFileSync('src/lib/projectId.ts', 'utf8')
    expect(src).not.toMatch(/from\s+['"]@\//)
  })

  it('parses positive integers only', () => {
    expect(parseProjectId('12')).toBe(12)
    expect(parseProjectId('0')).toBeNull()
    expect(parseProjectId(undefined)).toBeNull()
  })
})
