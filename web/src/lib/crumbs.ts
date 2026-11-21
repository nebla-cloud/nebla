import {
  computed,
  onScopeDispose,
  shallowRef,
  toValue,
  type ComputedRef,
  type MaybeRefOrGetter,
} from 'vue'
import type { RouteLocationRaw } from 'vue-router'

export type Crumb = { label: string; to?: RouteLocationRaw }

type Entry = { items: MaybeRefOrGetter<Crumb[]> }

const current = shallowRef<Entry | null>(null)

export function useCrumbs(items: MaybeRefOrGetter<Crumb[]>): void {
  const entry: Entry = { items }
  current.value = entry
  onScopeDispose(() => {
    if (current.value === entry) current.value = null
  })
}

export function useCurrentCrumbs(): ComputedRef<Crumb[] | null> {
  return computed(() => (current.value ? toValue(current.value.items) : null))
}
