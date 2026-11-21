import type { RouteRecordRaw } from 'vue-router'
import { volumesNavHidden } from './available'

export const side = 'project'

export default [
  {
    path: 'volumes',
    name: 'volumes',
    component: () => import('./VolumeListPage.vue'),
    meta: {
      side: 'project',
      nav: {
        label: 'Volumes',
        order: 12,
        icon: 'i-lucide-database',
        hidden: (route) => volumesNavHidden(route),
      },
    },
  },
  {
    path: 'volumes/:volumeId',
    name: 'volume',
    component: () => import('./VolumeDetailPage.vue'),
    meta: { side: 'project' },
  },
] satisfies RouteRecordRaw[]
