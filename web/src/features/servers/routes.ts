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
