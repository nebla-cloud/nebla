import type { RouteRecordRaw } from 'vue-router'
import volumeTabs from '@/features/volumes/serverTabs'

export const side = 'project'

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
    children: [
      {
        path: '',
        name: 'server',
        component: () => import('./tabs/OverviewTab.vue'),
        meta: { side: 'project' },
      },
      {
        path: 'graphs',
        name: 'server-graphs',
        component: () => import('./tabs/GraphsTab.vue'),
        meta: { side: 'project' },
      },
      {
        path: 'console',
        name: 'server-console',
        component: () => import('./tabs/ConsoleTab.vue'),
        meta: { side: 'project' },
      },
      {
        path: 'rebuild',
        name: 'server-rebuild',
        component: () => import('./tabs/RebuildTab.vue'),
        meta: { side: 'project' },
      },
      {
        path: 'resize',
        name: 'server-resize',
        component: () => import('./tabs/ResizeTab.vue'),
        meta: { side: 'project' },
      },
      ...volumeTabs, // redesign: remove, serverTabs.ts glob covers it
      {
        path: 'activity',
        name: 'server-activity',
        component: () => import('./tabs/ActivityTab.vue'),
        meta: { side: 'project' },
      },
      ...Object.values(import.meta.glob<{ default: RouteRecordRaw[] }>('../firewalls/serverTabs.ts', { eager: true })).flatMap((m) => m.default), // redesign: remove, serverTabs.ts glob covers it
    ],
  },
] satisfies RouteRecordRaw[]
