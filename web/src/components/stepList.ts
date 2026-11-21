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
