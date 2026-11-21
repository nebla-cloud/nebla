// Copyright (C) 2026 The Nebla contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

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
