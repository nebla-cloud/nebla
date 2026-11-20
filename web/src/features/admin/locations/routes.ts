import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'locations',
    name: 'admin-locations',
    component: () => import('./LocationsPage.vue'),
    meta: { side: 'admin', nav: { label: 'Locations', order: 10 } },
  },
  {
    path: 'locations/:locationId',
    name: 'admin-location',
    component: () => import('./LocationDetailPage.vue'),
  },
] satisfies RouteRecordRaw[]
