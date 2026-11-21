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
