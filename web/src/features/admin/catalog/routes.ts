import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'os-images',
    name: 'admin-catalog',
    component: () => import('./CatalogPage.vue'),
    meta: { side: 'admin', nav: { label: 'OS images', order: 20, icon: 'i-lucide-disc' } },
  },
] satisfies RouteRecordRaw[]
