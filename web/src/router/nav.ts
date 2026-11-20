import { computed, type ComputedRef } from 'vue'
import type { Role } from '@/api/types'
import { useCurrentProject } from '@/lib/project'
import { useRouter, type RouteRecordName, type Router } from 'vue-router'

export type Side = 'project' | 'admin' | 'root'

declare module 'vue-router' {
  interface RouteMeta {
    side?: Side
    nav?: { label: string; order: number; hidden?: () => boolean }
    public?: boolean
    roles?: Role[]
    guestOnly?: boolean
  }
}

export interface NavItem {
  name: RouteRecordName
  label: string
  order: number
}

export function navItems(router: Router, side: Exclude<Side, 'root'>, role?: Role): NavItem[] {
  return router
    .getRoutes()
    .filter(
      (r) =>
        r.meta.side === side &&
        r.meta.nav &&
        r.name &&
        !r.meta.nav.hidden?.() &&
        (!r.meta.roles || (role !== undefined && r.meta.roles.includes(role))),
    )
    .map((r) => ({ name: r.name!, label: r.meta.nav!.label, order: r.meta.nav!.order }))
    .sort((a, b) => a.order - b.order)
}

export function useNav(side: Exclude<Side, 'root'>): ComputedRef<NavItem[]> {
  const router = useRouter()
  const { project } = useCurrentProject()
  return computed(() => navItems(router, side, project.value?.role))
}
