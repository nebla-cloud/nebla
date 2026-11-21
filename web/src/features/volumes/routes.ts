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
