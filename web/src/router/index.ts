import { defineComponent } from 'vue'
import {
  createRouter,
  createWebHistory,
  type RouteRecordRaw,
  type Router,
  type RouterHistory,
} from 'vue-router'
import { redirectOnUnauthorized, setProjectId } from '@/api/client'
import { panelReady } from '@/features/admin/getready/checklist'
import { queryClient } from '@/lib/queryClient'
import { setMe } from '@/lib/session'
import { installGuards } from './guards'
import type { Side } from './nav'

interface RouteModule {
  default: RouteRecordRaw[]
  side: Side
}

const Empty = defineComponent({ render: () => null })

const modules = import.meta.glob<RouteModule>('../features/**/routes.ts', { eager: true })

function bySide(side: Side): RouteRecordRaw[] {
  return Object.values(modules)
    .filter((m) => m.side === side)
    .flatMap((m) => m.default)
}

export function createAppRouter(history: RouterHistory = createWebHistory()): Router {
  const router = createRouter({
    history,
    routes: [
      { path: '/', name: 'root', component: Empty },
      {
        path: '/p/:projectId',
        name: 'project',
        component: () => import('@/layouts/ProjectLayout.vue'),
        meta: { side: 'project' },
        children: [
          { path: '', redirect: (to) => `/p/${String(to.params.projectId)}/servers` },
          ...bySide('project'),
        ],
      },
      {
        path: '/admin',
        name: 'admin',
        component: () => import('@/layouts/AdminLayout.vue'),
        meta: { side: 'admin' },
        children: [
          {
            path: '',
            name: 'admin-home',
            component: Empty,
            beforeEnter: async () =>
              (await panelReady(queryClient)) === false
                ? { name: 'admin-get-ready' }
                : { name: 'admin-locations' },
          },
          ...bySide('admin'),
        ],
      },
      ...bySide('root'),
      {
        path: '/:pathMatch(.*)*',
        name: 'not-found',
        component: () => import('@/components/NotFoundPage.vue'),
      },
    ],
    scrollBehavior: (_to, _from, saved) => saved ?? { top: 0 },
  })
  installGuards(router)
  redirectOnUnauthorized(router, () => {
    setMe(null)
    setProjectId(null)
    queryClient.clear()
  })
  return router
}
