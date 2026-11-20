import type { RouteRecordRaw } from 'vue-router'

export const side = 'root'

export default [
  {
    path: '/account',
    name: 'account',
    component: () => import('./AccountPage.vue'),
    meta: { side: 'root' },
  },
] satisfies RouteRecordRaw[]
