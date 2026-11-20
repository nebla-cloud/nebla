import type { RouteRecordRaw } from 'vue-router'

// The pop-out console sits outside the project layout so it renders without the shell.
export const side = 'root'

export default [
  {
    path: '/console/:projectId/:serverId',
    name: 'server-console-window',
    component: () => import('./ConsoleWindowPage.vue'),
    meta: { side: 'root' },
  },
] satisfies RouteRecordRaw[]
