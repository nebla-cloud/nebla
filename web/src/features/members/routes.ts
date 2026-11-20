import type { RouteRecordRaw } from 'vue-router'

export const side = 'project'

export default [
  {
    path: 'members',
    name: 'members',
    component: () => import('./MembersPage.vue'),
    meta: { side: 'project', nav: { label: 'Members', order: 40 } },
  },
] satisfies RouteRecordRaw[]
