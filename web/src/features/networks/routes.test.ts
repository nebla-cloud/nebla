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
import { createMemoryHistory } from 'vue-router'
import { createAppRouter } from '@/router'
import { navItems } from '@/router/nav'

describe('network routes', () => {
  it('puts Networks right after Floating IPs in the project menu', () => {
    const router = createAppRouter(createMemoryHistory())
    const items = navItems(router, 'project', 'owner')
    const labels = items.map((i) => i.label)
    expect(labels.filter((l) => l === 'Networks')).toHaveLength(1)
    expect(labels.indexOf('Networks')).toBe(labels.indexOf('Floating IPs') + 1)
    expect(items.find((i) => i.label === 'Networks')!.icon).toBe('i-lucide-network')
  })

  it('resolves the list, detail and server tab', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/networks').name).toBe('networks')
    expect(router.resolve('/p/1/networks/3').name).toBe('network')
    const tab = router.resolve('/p/1/servers/2/networking')
    expect(tab.name).toBe('server-networking')
    expect(tab.matched.at(-1)!.meta.serverTab).toEqual({ label: 'Networking', order: 45 })
  })
})
