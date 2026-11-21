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

import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export const builtInTabs: RouteRecordRaw[] = [
  {
    path: '',
    name: 'server',
    component: () => import('./tabs/OverviewTab.vue'),
    meta: { side: 'project', serverTab: { label: 'Overview', order: 10 } },
  },
  {
    path: 'graphs',
    name: 'server-graphs',
    component: () => import('./tabs/GraphsTab.vue'),
    meta: { side: 'project', serverTab: { label: 'Graphs', order: 20 } },
  },
  {
    path: 'console',
    name: 'server-console',
    component: () => import('./tabs/ConsoleTab.vue'),
    meta: { side: 'project', serverTab: { label: 'Console', order: 30 } },
  },
  {
    path: 'rebuild',
    name: 'server-rebuild',
    component: () => import('./tabs/RebuildTab.vue'),
    meta: { side: 'project', serverTab: { label: 'Rebuild', order: 40 } },
  },
  {
    path: 'resize',
    name: 'server-resize',
    component: () => import('./tabs/ResizeTab.vue'),
    meta: { side: 'project', serverTab: { label: 'Resize', order: 50 } },
  },
  {
    path: 'activity',
    name: 'server-activity',
    component: () => import('./tabs/ActivityTab.vue'),
    meta: { side: 'project', serverTab: { label: 'Activity', order: 60 } },
  },
]

// Other features add server tabs by exporting child routes from features/<feature>/serverTabs.ts.
const extraTabs = import.meta.glob<{ default: RouteRecordRaw[] }>('../*/serverTabs.ts', {
  eager: true,
})

export function withServerTabs(
  children: RouteRecordRaw[],
  extra: Record<string, { default: RouteRecordRaw[] }>,
): RouteRecordRaw[] {
  return [...children, ...Object.values(extra).flatMap((m) => m.default)]
}

export default [
  {
    path: 'servers',
    name: 'servers',
    component: () => import('./ServerListPage.vue'),
    meta: { side: 'project', nav: { label: 'Servers', order: 10, icon: 'i-lucide-server' } },
  },
  {
    path: 'servers/:serverId',
    component: () => import('./ServerDetailShell.vue'),
    meta: { side: 'project' },
    children: withServerTabs(builtInTabs, extraTabs),
  },
] satisfies RouteRecordRaw[]
