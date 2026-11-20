import { watch, type Ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

// Moves an empty page past the end (say, after deleting its last row) back to the last page.
export function usePageClamp(
  page: Ref<number>,
  rows: Ref<number | undefined>,
  total: Ref<number>,
  perPage: number,
) {
  const route = useRoute()
  const router = useRouter()
  watch(
    [page, rows, total],
    ([p, n, t]) => {
      if (n !== 0 || p <= 1) return
      const last = Math.max(1, Math.ceil(t / perPage))
      if (last >= p) return
      const query = { ...route.query, page: String(last) }
      if (last === 1) delete (query as Record<string, unknown>).page
      void router.replace({ query })
    },
    { immediate: true },
  )
}
