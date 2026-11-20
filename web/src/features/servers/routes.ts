import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'servers',
    name: 'servers',
    component: () => import('./ServerListPage.vue'),
    meta: { side: 'project', nav: { label: 'Servers', order: 10 } },
  },
  {
    path: 'servers/:serverId',
    name: 'server',
    component: () => import('./ServerDetailPage.vue'),
    meta: { side: 'project' },
  },
] satisfies RouteRecordRaw[]
