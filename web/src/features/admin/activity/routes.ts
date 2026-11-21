import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'activity',
    name: 'admin-activity',
    component: () => import('./AdminActivityPage.vue'),
    meta: { side: 'admin', nav: { label: 'Activity', order: 70, icon: 'i-lucide-activity', group: 'System' } },
  },
] satisfies RouteRecordRaw[]
