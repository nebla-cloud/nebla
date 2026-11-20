import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'servers/new',
    name: 'server-new',
    component: () => import('./CreateServerPage.vue'),
    meta: { side: 'project' },
  },
] satisfies RouteRecordRaw[]
