import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'firewalls',
    name: 'firewalls',
    component: () => import('./FirewallsPage.vue'),
    meta: { side: 'project', nav: { label: 'Firewalls', order: 14, icon: 'i-lucide-shield' } },
  },
  {
    path: 'firewalls/:firewallId',
    name: 'firewall',
    component: () => import('./FirewallDetailPage.vue'),
    meta: { side: 'project' },
  },
] satisfies RouteRecordRaw[]
