import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'settings',
    name: 'admin-settings',
    component: () => import('./SettingsPage.vue'),
    meta: { side: 'admin', nav: { label: 'Settings', order: 80, icon: 'i-lucide-settings', group: 'System' } },
  },
] satisfies RouteRecordRaw[]
