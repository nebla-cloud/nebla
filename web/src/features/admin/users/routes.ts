import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'users',
    name: 'admin-users',
    component: () => import('./UsersPage.vue'),
    meta: { side: 'admin', nav: { label: 'Users', order: 50 } },
  },
] satisfies RouteRecordRaw[]
