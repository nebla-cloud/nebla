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

export default [
  {
    path: 'backups',
    name: 'server-backups',
    component: () => import('./BackupsTab.vue'),
    meta: {
      serverTab: {
        label: 'Backups',
        order: 57,
        // Kept while backups exist, so they stay restorable after the location stops offering them.
        visible: (location, server) =>
          location?.backups_available === true || server.backups.count > 0,
      },
    },
  },
] satisfies RouteRecordRaw[]
