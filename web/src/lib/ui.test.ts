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
import * as ui from './ui'
import { selectClass, table, tableWrap, tbody, thead } from './ui'

const tokens = (s: string) => s.split(/\s+/).filter(Boolean)

describe('ui classes', () => {
  it('keeps the stack-on-mobile table pattern', () => {
    expect(tokens(table)).toEqual(expect.arrayContaining(['block', 'sm:table']))
    expect(tokens(thead)).toEqual(
      expect.arrayContaining(['hidden', 'bg-(--nb-soft)', 'text-muted', 'sm:table-header-group']),
    )
    expect(tokens(tbody)).toContain('sm:table-row-group')
    expect(tokens(tableWrap)).toContain('overflow-hidden')
  })

  it('has one select style on the input border', () => {
    expect(tokens(selectClass)).toEqual(expect.arrayContaining(['border-default', 'h-9']))
  })

  it('never glues two utilities together', () => {
    for (const [name, value] of Object.entries(ui)) {
      for (const t of tokens(value as string)) {
        expect(t, `${name}: ${t}`).not.toMatch(/\)[A-Za-z]/)
      }
    }
  })
})
