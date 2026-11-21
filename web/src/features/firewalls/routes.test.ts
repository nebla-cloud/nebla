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

describe('firewalls routes', () => {
  it('replaces the Soon entry with a real Firewalls nav item', () => {
    const router = createAppRouter(createMemoryHistory())
    const items = navItems(router, 'project', 'owner').filter((i) => i.label === 'Firewalls')
    expect(items).toHaveLength(1)
    expect(items[0]!.soon).toBeUndefined()
    expect(items[0]!.icon).toBe('i-lucide-shield')
  })

  it('resolves the list, detail and server tab', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/firewalls').name).toBe('firewalls')
    expect(router.resolve('/p/1/firewalls/3').name).toBe('firewall')
    expect(router.resolve('/p/1/servers/2/firewalls').name).toBe('server-firewalls')
  })
})
