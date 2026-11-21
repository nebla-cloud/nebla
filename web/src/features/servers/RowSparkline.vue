<!--
Copyright (C) 2026 The Nebla contributors

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.
-->

<script lang="ts">
// Shared by every row so a long list never has more than 20 metric requests open.
const MAX_OPEN = 20
let open = 0
const waiting: (() => void)[] = []

async function limited<T>(fn: () => Promise<T>): Promise<T> {
  if (open < MAX_OPEN) open++
  else await new Promise<void>((r) => waiting.push(r))
  try {
    return await fn()
  } finally {
    const next = waiting.shift()
    if (next) next()
    else open--
  }
}
</script>

<script setup lang="ts">
import { computed, useTemplateRef, watch } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { useDocumentVisibility, useElementVisibility } from '@vueuse/core'
import Sparkline from '@/components/Sparkline.vue'
import { useCurrentProject } from '@/lib/project'
import { fetchMetrics, formatPercent } from './metrics/metrics'

const props = defineProps<{ serverId: number; running: boolean }>()

const el = useTemplateRef<HTMLElement>('el')
const visible = useElementVisibility(el)
const documentVisibility = useDocumentVisibility()
const { projectId } = useCurrentProject()
const qc = useQueryClient()
const queryKey = computed(() => ['server-metrics', projectId.value, props.serverId, 'hour', 'row'] as const)
const enabled = computed(
  () => visible.value && documentVisibility.value === 'visible' && props.running && projectId.value > 0,
)

watch(enabled, (active) => {
  if (!active) void qc.cancelQueries({ queryKey: queryKey.value, exact: true })
})

const query = useQuery({
  // 'row' keeps this apart from the Overview's live cache.
  queryKey,
  queryFn: ({ queryKey: [, , serverId], signal }) =>
    limited(() => {
      signal.throwIfAborted()
      return fetchMetrics(serverId, 'hour', ['cpu'], signal)
    }),
  enabled,
  retry: false,
  refetchInterval: 120_000,
  refetchIntervalInBackground: false,
  staleTime: 110_000,
  refetchOnWindowFocus: false,
})

const points = computed(() => {
  if (!props.running || query.isError.value) return null
  const cpu = query.data.value?.time_series?.cpu as [number, number | null][] | undefined
  return cpu && cpu.filter((p) => p[1] != null).length >= 2 ? cpu : null
})
const latest = computed(() => {
  const pts = points.value ?? []
  for (let i = pts.length - 1; i >= 0; i--) if (pts[i]![1] != null) return pts[i]![1]
  return null
})
</script>

<template>
  <span ref="el" class="block">
    <span v-if="points" class="flex items-center gap-2">
      <Sparkline
        :lines="[{ points, tone: 'busy' }]"
        :width="80"
        :height="22"
        :max="100"
        class="min-w-0 flex-1"
      />
      <span class="w-8 shrink-0 text-right text-xs tabular-nums">{{
        formatPercent(latest ?? 0)
      }}</span>
    </span>
    <span v-else aria-hidden="true" class="text-muted">—</span>
  </span>
</template>
