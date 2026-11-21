import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'ip-pools',
    name: 'admin-ip-pools',
    component: () => import('./IpPoolsPage.vue'),
    meta: { side: 'admin', nav: { label: 'IP pools', order: 40, icon: 'i-lucide-network' } },
  },
] satisfies RouteRecordRaw[]
