import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'server-types',
    name: 'admin-server-types',
    component: () => import('./ServerTypesPage.vue'),
    meta: { side: 'admin', nav: { label: 'Server types', order: 30 } },
  },
] satisfies RouteRecordRaw[]
