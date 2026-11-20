import { computed, type ComputedRef } from 'vue'
import type { QueryClient } from '@tanstack/vue-query'

export function usePanelReady(): ComputedRef<boolean | undefined> {
  return computed(() => true)
}

export async function panelReady(qc: QueryClient): Promise<boolean> {
  void qc
  return true
}
