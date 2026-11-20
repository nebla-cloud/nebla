import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'activity',
    name: 'activity',
    component: () => import('./ActivityPage.vue'),
    meta: { side: 'project', nav: { label: 'Activity', order: 30 } },
  },
] satisfies RouteRecordRaw[]
