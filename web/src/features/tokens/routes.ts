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
      nav: { label: 'API tokens', order: 25, icon: 'i-lucide-square-code', group: 'Access' },
    },
  },
] satisfies RouteRecordRaw[]
