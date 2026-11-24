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

import { computed, type ComputedRef } from 'vue'
import type { Location, Role, Server } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import {
  useRouter,
  type RouteLocationNormalizedLoaded,
  type RouteRecordName,
  type Router,
} from 'vue-router'

export type Side = 'project' | 'admin' | 'root'

declare module 'vue-router' {
  interface RouteMeta {
    side?: Side
    nav?: {
      label: string
      order: number
      icon?: string
      group?: string
      hidden?: (route: RouteLocationNormalizedLoaded) => boolean
    }
    public?: boolean
    roles?: Role[]
    guestOnly?: boolean
    serverTab?: {
      label: string
      order: number
      // Hides the tab; the location is undefined until the project's locations load.
      visible?: (location: Location | undefined, server: Server) => boolean
    }
  }
}

export interface NavItem {
  name: RouteRecordName | null
  label: string
  order: number
  icon: string
  group: string | null
  soon?: boolean
}

const GROUP_ORDER: (string | null)[] = [null, 'Access', 'People', 'Project', 'System']
const groupRank = (g: string | null) => {
  const i = GROUP_ORDER.indexOf(g)
  return i === -1 ? GROUP_ORDER.length : i
}

const SOON: NavItem[] = [
  { label: 'Volumes', icon: 'i-lucide-database', order: 12, group: null, soon: true, name: null },
  { label: 'Firewalls', icon: 'i-lucide-shield', order: 14, group: null, soon: true, name: null },
]

export function navItems(router: Router, side: Exclude<Side, 'root'>, role?: Role): NavItem[] {
  const route = router.currentRoute.value
  const onSide = router.getRoutes().filter((r) => r.meta.side === side && r.meta.nav && r.name)
  const real: NavItem[] = onSide
    .filter(
      (r) =>
        !r.meta.nav!.hidden?.(route) &&
        (!r.meta.roles || (role !== undefined && r.meta.roles.includes(role))),
    )
    .map((r) => ({
      name: r.name!,
      label: r.meta.nav!.label,
      order: r.meta.nav!.order,
      icon: r.meta.nav!.icon ?? 'i-lucide-circle-dot',
      group: r.meta.nav!.group ?? null,
    }))
  // A real route replaces its soon entry, even while the route itself is hidden.
  const soon =
    side === 'project' ? SOON.filter((s) => !onSide.some((r) => r.meta.nav!.label === s.label)) : []
  return [...real, ...soon].sort(
    (a, b) => groupRank(a.group) - groupRank(b.group) || a.order - b.order,
  )
}

// The page is the item's own path or somewhere under it.
export function isActive(
  router: Router,
  route: RouteLocationNormalizedLoaded,
  item: NavItem,
): boolean {
  if (item.name === null) return false
  const href = router.resolve({ name: item.name, params: route.params }).path
  return route.path === href || route.path.startsWith(`${href}/`)
}

export function useNav(side: Exclude<Side, 'root'>): ComputedRef<NavItem[]> {
  const router = useRouter()
  const project = side === 'project' ? useCurrentProject().project : undefined
  return computed(() => navItems(router, side, project?.value?.role))
}
