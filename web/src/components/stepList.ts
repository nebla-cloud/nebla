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

import type { ComputedRef, InjectionKey, Ref } from 'vue'

export type StepState = 'done' | 'current' | 'todo'

export interface StepEntry {
  el: Ref<HTMLElement | null>
  done: () => boolean | undefined
}

export interface StepListContext {
  register(entry: StepEntry): { n: ComputedRef<number>; state: ComputedRef<StepState> }
  unregister(entry: StepEntry): void
  sort(): void
}

export const stepListKey: InjectionKey<StepListContext> = Symbol('stepList')
