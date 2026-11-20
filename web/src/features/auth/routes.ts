import type { RouteRecordRaw } from 'vue-router'
import { notFound } from '@/router/guards'
import { useSession } from '@/lib/session'

export const side = 'root'

export default [
  {
    path: '/login',
    name: 'login',
    component: () => import('./LoginPage.vue'),
    meta: { public: true },
  },
  {
    path: '/signup',
    name: 'signup',
    component: () => import('./SignupPage.vue'),
    meta: { side: 'root', public: true, guestOnly: true },
    beforeEnter: (to) => (useSession().manifest.value?.signup_enabled ? true : notFound(to)),
  },
  {
    path: '/invite/:token',
    name: 'invite',
    component: () => import('./InvitePage.vue'),
    meta: { side: 'root', public: true },
  },
  {
    path: '/setup',
    name: 'setup',
    component: () => import('./SetupPage.vue'),
    meta: { public: true },
  },
] satisfies RouteRecordRaw[]
