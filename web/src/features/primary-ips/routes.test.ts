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

describe('primary IP routes', () => {
  it('puts Primary IPs right after Floating IPs in the project menu', () => {
    const router = createAppRouter(createMemoryHistory())
    const labels = navItems(router, 'project', 'owner').map((i) => i.label)
    expect(labels.filter((l) => l === 'Primary IPs')).toHaveLength(1)
    expect(labels.indexOf('Primary IPs')).toBe(labels.indexOf('Floating IPs') + 1)
  })

  it('resolves the list and detail', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/primary-ips').name).toBe('primary-ips')
    expect(router.resolve('/p/1/primary-ips/3').name).toBe('primary-ip')
  })
})
