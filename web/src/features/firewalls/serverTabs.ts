import type { RouteRecordRaw } from 'vue-router'

export default [
  {
    path: 'firewalls',
    name: 'server-firewalls',
    component: () => import('./FirewallsTab.vue'),
    meta: { serverTab: { label: 'Firewalls', order: 45 } },
  },
] satisfies RouteRecordRaw[]
