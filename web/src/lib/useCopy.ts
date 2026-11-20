import { onScopeDispose, ref, type Ref } from 'vue'

export function useCopy(): {
  copied: Ref<boolean>
  copy: (text: string, fallback?: HTMLElement | null) => Promise<boolean>
} {
  const copied = ref(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  onScopeDispose(() => clearTimeout(timer))

  async function copy(text: string, fallback?: HTMLElement | null): Promise<boolean> {
    try {
      await navigator.clipboard.writeText(text)
      copied.value = true
      clearTimeout(timer)
      timer = setTimeout(() => (copied.value = false), 2000)
      return true
    } catch {
      if (fallback instanceof HTMLInputElement || fallback instanceof HTMLTextAreaElement) {
        fallback.focus()
        fallback.select()
      } else if (fallback) {
        const range = document.createRange()
        range.selectNodeContents(fallback)
        const sel = window.getSelection()
        sel?.removeAllRanges()
        sel?.addRange(range)
      }
      return false
    }
  }
  return { copied, copy }
}
