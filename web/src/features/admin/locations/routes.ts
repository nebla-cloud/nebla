import type { RouteRecordRaw } from 'vue-router'

export const side = 'admin'

export default [
  {
    path: 'locations',
    name: 'admin-locations',
    component: () => import('./LocationsPage.vue'),
    meta: { side: 'admin', nav: { label: 'Locations', order: 10, icon: 'i-lucide-map-pin' } },
  },
  {
    path: 'locations/enroll/:enrollmentId',
    name: 'admin-location-enroll',
    component: () => import('./EnrollPage.vue'),
  },
  {
    path: 'locations/:locationId',
    name: 'admin-location',
    component: () => import('./LocationDetailPage.vue'),
  },
] satisfies RouteRecordRaw[]
