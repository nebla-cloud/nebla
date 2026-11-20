import { computed, type ComputedRef } from 'vue'
import { useRouter, type RouteRecordName, type Router } from 'vue-router'

export type Side = 'project' | 'admin' | 'root'

declare module 'vue-router' {
  interface RouteMeta {
    side?: Side
    nav?: { label: string; order: number; hidden?: () => boolean }
    public?: boolean
  }
}

export interface NavItem {
  name: RouteRecordName
  label: string
  order: number
}

export function navItems(router: Router, side: Exclude<Side, 'root'>): NavItem[] {
  return router
    .getRoutes()
    .filter((r) => r.meta.side === side && r.meta.nav && r.name && !r.meta.nav.hidden?.())
    .map((r) => ({ name: r.name!, label: r.meta.nav!.label, order: r.meta.nav!.order }))
    .sort((a, b) => a.order - b.order)
}

export function useNav(side: Exclude<Side, 'root'>): ComputedRef<NavItem[]> {
  const router = useRouter()
  return computed(() => navItems(router, side))
}
