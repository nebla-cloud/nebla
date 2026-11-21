import type { RouteRecordRaw } from 'vue-router'

export default [
  {
    path: 'volumes',
    name: 'server-volumes',
    component: () => import('./VolumesSection.vue'),
    meta: { serverTab: { label: 'Volumes', order: 55 } },
  },
] satisfies RouteRecordRaw[]
