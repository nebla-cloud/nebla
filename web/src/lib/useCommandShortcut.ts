import type { Ref } from 'vue'
import { useEventListener } from '@vueuse/core'

const SKIP =
  'input, textarea, select, [contenteditable]:not([contenteditable="false"]), [data-console]'

// Toggles `open` on Ctrl+K or ⌘K, leaving fields and the VM console their own keys.
export function useCommandShortcut(open: Ref<boolean>): void {
  useEventListener(document, 'keydown', (e: KeyboardEvent) => {
    if (!(e.metaKey || e.ctrlKey) || e.shiftKey || e.altKey || e.repeat) return
    if (e.key.toLowerCase() !== 'k' || e.defaultPrevented) return
    if (e.target instanceof Element && e.target.closest(SKIP)) return
    e.preventDefault()
    open.value = !open.value
  })
}
