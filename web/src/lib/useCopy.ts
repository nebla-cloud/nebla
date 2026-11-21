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
