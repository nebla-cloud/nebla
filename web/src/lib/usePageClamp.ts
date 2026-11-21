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
