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
import { RouterView, createMemoryHistory, createRouter, type RouteRecordRaw } from 'vue-router'
import { createAppRouter } from '@/router'
import routes, { builtInTabs, withServerTabs } from './routes'

const firewalls: RouteRecordRaw = {
  path: 'firewalls',
  name: 'server-firewalls',
  component: RouterView,
  meta: { side: 'project', serverTab: { label: 'Firewalls', order: 45 } },
}
const extra = { '../firewalls/serverTabs.ts': { default: [firewalls] } }

function routerWith(more: Record<string, { default: RouteRecordRaw[] }>) {
  const servers = routes.map((r) =>
    'children' in r ? { ...r, children: withServerTabs(builtInTabs, more) } : r,
  )
  return createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/p/:projectId', component: RouterView, children: servers }],
  })
}

describe('server routes', () => {
  it('appends serverTabs.ts children after the built-in tabs', () => {
    expect(withServerTabs(builtInTabs, extra).map((c) => c.name)).toEqual([
      'server',
      'server-graphs',
      'server-console',
      'server-rebuild',
      'server-resize',
      'server-activity',
      'server-firewalls',
    ])
  })

  it('an extra tab sorts by order and Overview keeps the bare server path', () => {
    const router = routerWith(extra)
    expect(router.resolve('/p/1/servers/5').name).toBe('server')
    expect(router.resolve('/p/1/servers/5/firewalls').name).toBe('server-firewalls')
    const tabs = router
      .getRoutes()
      .filter((r) => r.meta.serverTab)
      .sort((a, b) => a.meta.serverTab!.order - b.meta.serverTab!.order)
      .map((r) => r.meta.serverTab!.label)
    expect(tabs).toEqual([
      'Overview',
      'Graphs',
      'Console',
      'Rebuild',
      'Firewalls',
      'Resize',
      'Activity',
    ])
  })

  it('the app router has one server parent and resolves Overview', () => {
    const router = createAppRouter(createMemoryHistory())
    expect(router.resolve('/p/1/servers/5').name).toBe('server')
    // The Overview child shares this path; the parent is the record with children.
    const parents = router
      .getRoutes()
      .filter((r) => r.path === '/p/:projectId/servers/:serverId' && r.children.length)
    expect(parents).toHaveLength(1)
    expect(parents[0]!.children.map((c) => c.name)).toContain('server')
  })
})
