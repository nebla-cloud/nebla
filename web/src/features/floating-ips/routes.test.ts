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

describe('floating IP routes', () => {
  it('puts Floating IPs right after Firewalls in the project menu', () => {
    const router = createAppRouter(createMemoryHistory())
    const labels = navItems(router, 'project', 'owner').map((i) => i.label)
    expect(labels.filter((l) => l === 'Floating IPs')).toHaveLength(1)
    expect(labels.indexOf('Floating IPs')).toBe(labels.indexOf('Firewalls') + 1)
    const item = navItems(router, 'project', 'owner').find((i) => i.label === 'Floating IPs')!
    expect(item.icon).toBe('i-lucide-locate-fixed')
    expect(item.soon).toBeUndefined()
  })

  it('resolves the list and detail', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/floating-ips').name).toBe('floating-ips')
    expect(router.resolve('/p/1/floating-ips/3').name).toBe('floating-ip')
  })
})
