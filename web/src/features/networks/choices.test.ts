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
import { makeServer } from '@/features/servers/fixtures'
import { serverChoices } from './choices'

const onNet = (network: number) => ({ network, name: 'n', ip: '10.0.0.2', guest_config: null })

describe('serverChoices', () => {
  it('keeps only servers in the network location and says why one is disabled', () => {
    const far = makeServer({ id: 2, location: { id: 2, name: 'far', description: '' } })
    const joined = makeServer({ id: 3, private_net: [onNet(1)] })
    const full = makeServer({ id: 4, private_net: [onNet(2), onNet(3), onNet(4)] })
    const busy = makeServer({ id: 5, status: 'rebuilding' })
    const free = makeServer({ id: 6 })
    const out = serverChoices([far, joined, full, busy, free], 1, 1)
    expect(out.map((c) => [c.server.id, c.disabledReason])).toEqual([
      [3, 'Already on this network'],
      [4, 'Already on 3 networks'],
      [5, 'Busy right now'],
      [6, null],
    ])
  })
})
