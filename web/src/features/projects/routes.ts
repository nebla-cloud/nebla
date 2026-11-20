import type { RouteRecordRaw } from 'vue-router'

export const side = 'root'

export default [
  { path: '/projects', name: 'projects', component: () => import('./ProjectsPage.vue') },
] satisfies RouteRecordRaw[]
