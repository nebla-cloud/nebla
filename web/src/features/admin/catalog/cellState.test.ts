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
import type { CatalogLocationState, CatalogNode } from './queries'
import { cellActions, cellView } from './cellState'

const node = (status: string, n = 'pve1'): CatalogNode => ({
  node: n,
  status,
  vmid: null,
  file: '',
  built_at: null,
  error: '',
})

const loc = (state: string, extra: Partial<CatalogLocationState> = {}): CatalogLocationState => ({
  location_id: 1,
  available: true,
  reason: '',
  state,
  nodes: [],
  ...extra,
})

describe('cellView', () => {
  it('cellView maps every state', () => {
    const cases: [CatalogLocationState, string, string, boolean, string | null][] = [
      [loc('off'), 'Off', 'neutral', false, 'enable'],
      [loc('building', { nodes: [node('building')] }), 'Building', 'neutral', true, null],
      [loc('ready', { nodes: [node('ready')] }), 'Ready', 'success', false, 'refresh'],
      [loc('failed', { nodes: [node('failed')] }), 'Failed', 'error', false, 'retry'],
      [
        loc('partial', {
          nodes: [node('ready', 'pve1'), node('ready', 'pve2'), node('failed', 'pve3')],
        }),
        '2 of 3 nodes',
        'neutral',
        false,
        'retry',
      ],
      [loc('retired'), 'Retired', 'muted', false, null],
      [
        loc('unavailable', { available: false, reason: 'no import storage' }),
        'Unavailable: no import storage',
        'muted',
        false,
        null,
      ],
      [loc('melting'), 'melting', 'muted', false, null],
    ]
    for (const [input, label, tone, pulse, primary] of cases) {
      expect(cellView(input), input.state).toMatchObject({ label, tone, pulse, primary })
      expect(typeof cellView(input).detail).toBe('string')
    }
  })

  it('partial with no failed node says it is not on every node', () => {
    const v = cellView(loc('partial', { nodes: [node('ready', 'pve1'), node('ready', 'pve2')] }))
    expect(v.label).toBe('Not on every node')
    expect(v.primary).toBe('retry')
    expect(v.detail).not.toContain('failed')
  })

  it('partial offers refresh only when a node is ready', () => {
    expect(cellActions(loc('partial', { nodes: [node('ready'), node('failed', 'pve2')] }))).toEqual(
      ['retry', 'refresh'],
    )
    expect(cellActions(loc('partial', { nodes: [node('failed')] }))).toEqual(['retry'])
  })

  it('retired entries offer no build actions', () => {
    expect(cellView(loc('off'), true)).toMatchObject({
      label: 'Not built',
      tone: 'muted',
      primary: null,
    })
    expect(cellActions(loc('off'), true)).toEqual([])
    expect(cellActions(loc('retired', { nodes: [node('ready')] }), true)).toEqual([])
  })

  it('shows the reason when a built cell is no longer available', () => {
    const v = cellView(
      loc('ready', { available: false, reason: 'no import storage', nodes: [node('ready')] }),
    )
    expect(v.label).toBe('Ready')
    expect(v.detail).toContain('no import storage')
  })
})
