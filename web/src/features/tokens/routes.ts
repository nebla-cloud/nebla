import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'tokens',
    name: 'tokens',
    component: () => import('./TokensPage.vue'),
    meta: {
      side: 'project',
      roles: ['owner', 'admin'],
      nav: { label: 'API tokens', order: 25 },
    },
  },
] satisfies RouteRecordRaw[]
