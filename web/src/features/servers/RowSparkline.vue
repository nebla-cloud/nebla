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
import { fetchMetrics } from './metrics/metrics'

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
</script>

<template>
  <span ref="el" aria-hidden="true" class="block">
    <Sparkline v-if="points" :points="points" :width="120" :height="22" />
    <span v-else class="text-muted">—</span>
  </span>
</template>
