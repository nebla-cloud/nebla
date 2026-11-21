import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'projects',
    name: 'admin-projects',
    component: () => import('./ProjectsPage.vue'),
    meta: { side: 'admin', nav: { label: 'Projects', order: 60, icon: 'i-lucide-folder', group: 'People' } },
  },
  {
    path: 'projects/:projectId',
    name: 'admin-project',
    component: () => import('./ProjectDetailPage.vue'),
    meta: { side: 'admin' },
  },
] satisfies RouteRecordRaw[]
