import type { RouteRecordRaw } from 'vue-router'
const featureTabs = Object.values(
  import.meta.glob<{ default: RouteRecordRaw[] }>('../*/serverTabs.ts', { eager: true }),
).flatMap((module) => module.default)

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
      ...featureTabs,
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
    ],
  },
] satisfies RouteRecordRaw[]
