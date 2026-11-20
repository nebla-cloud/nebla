import type { RouteRecordRaw } from 'vue-router'

export const side = 'root'

export default [
  {
    path: '/login',
    name: 'login',
    component: () => import('./LoginPage.vue'),
    meta: { public: true },
  },
  {
    path: '/setup',
    name: 'setup',
    component: () => import('./SetupPage.vue'),
    meta: { public: true },
  },
] satisfies RouteRecordRaw[]
