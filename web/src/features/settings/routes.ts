import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'settings',
    name: 'project-settings',
    component: () => import('./SettingsPage.vue'),
    meta: { side: 'project', nav: { label: 'Settings', order: 50 } },
  },
] satisfies RouteRecordRaw[]
