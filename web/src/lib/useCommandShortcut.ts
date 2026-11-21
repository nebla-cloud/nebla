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
