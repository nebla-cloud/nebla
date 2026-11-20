import type { RouteRecordRaw } from 'vue-router'
import { panelReadyKnown } from './checklist'

export const side = 'admin'

export default [
  {
    path: 'get-ready',
    name: 'admin-get-ready',
    component: () => import('./GetReadyPage.vue'),
    meta: {
      side: 'admin',
      nav: { label: 'Get ready', order: 0, hidden: () => panelReadyKnown.value === true },
    },
  },
] satisfies RouteRecordRaw[]
