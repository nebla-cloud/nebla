import { ref, type Ref } from 'vue'

// Which moment is hovered, and which chart the pointer is on.
export type HoverState = { t: number | null; source: symbol | null }

const states = new Map<string, Ref<HoverState>>()

// One shared hover state per key, so charts with the same key follow each other.
export function useHoverSync(key: string): Ref<HoverState> {
  let s = states.get(key)
  if (!s) {
    s = ref<HoverState>({ t: null, source: null })
    states.set(key, s)
  }
  return s
}
