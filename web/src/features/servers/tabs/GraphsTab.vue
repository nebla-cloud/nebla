<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useQuery } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import EmptyState from '@/components/EmptyState.vue'
import { useCurrentProject } from '@/lib/project'
import MetricChart from '../metrics/MetricChart.vue'
import { RANGES, parseRange, toSeries, type Range } from '../metrics/metrics'

const props = defineProps<{ server: Server }>()
defineEmits<{ started: [action: Action] }>()

const route = useRoute()
const router = useRouter()
const { projectId } = useCurrentProject()

const range = computed(() => parseRange(route.query.range))
function setRange(r: Range) {
  void router.replace({ query: { ...route.query, range: r } })
}

const REFRESH_MS = 60_000
const query = useQuery({
  queryKey: computed(() => ['server-metrics', projectId.value, props.server.id, range.value]),
  queryFn: async () =>
    unwrap(
      await api.GET('/servers/{id}/metrics', {
        params: {
          path: { id: props.server.id },
          query: { type: ['cpu', 'disk', 'network'], timeframe: range.value },
        },
      }),
    ).metrics,
  enabled: computed(() => projectId.value > 0),
  retry: false,
  refetchInterval: () =>
    range.value === 'hour' && document.visibilityState === 'visible' ? REFRESH_MS : false,
})

const charts = computed(() => {
  const ts = (query.data.value?.time_series ?? {}) as Record<string, [number, number | null][]>
  const make = (
    title: string,
    unit: 'percent' | 'rate',
    parts: { key: string; label: string; tone: 'primary' | 'secondary' }[],
  ) => ({
    title,
    unit,
    series: parts.map(({ label, tone }) => ({ label, tone })),
    data: toSeries(
      ts,
      parts.map((p) => p.key),
    ),
  })
  return [
    make('CPU', 'percent', [{ key: 'cpu', label: 'CPU', tone: 'primary' }]),
    make('Disk', 'rate', [
      { key: 'disk.read', label: 'Read', tone: 'primary' },
      { key: 'disk.write', label: 'Write', tone: 'secondary' },
    ]),
    make('Network', 'rate', [
      { key: 'network.in', label: 'In', tone: 'primary' },
      { key: 'network.out', label: 'Out', tone: 'secondary' },
    ]),
  ]
})

const hasData = computed(() =>
  charts.value.some((c) => c.data.slice(1).some((col) => col.some((v) => v != null))),
)
const noDataYet = computed(() => {
  const e = query.error.value
  return e instanceof ApiError && e.status === 409
})
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <div role="group" aria-label="Time range" class="flex gap-1">
        <UButton
          v-for="r in RANGES"
          :key="r.value"
          size="sm"
          :color="range === r.value ? 'primary' : 'neutral'"
          :variant="range === r.value ? 'solid' : 'outline'"
          :aria-pressed="range === r.value"
          @click="setRange(r.value)"
        >
          {{ r.label }}
        </UButton>
      </div>
    </div>

    <p v-if="server.status === 'off'" class="text-sm text-muted">
      The server is off. Graphs resume when it starts.
    </p>

    <p v-if="query.isPending.value" class="text-sm text-muted">Loading graphs</p>
    <EmptyState
      v-else-if="noDataYet"
      title="No graph data yet"
      :body="errorMessage(query.error.value)"
    />
    <UAlert
      v-else-if="query.error.value"
      color="error"
      variant="subtle"
      title="Couldn't load graphs"
      :description="errorMessage(query.error.value)"
      :actions="[{ label: 'Try again', onClick: () => query.refetch() }]"
    />
    <EmptyState v-else-if="!hasData" title="No data for this range" body="Try a longer range." />
    <div v-else class="grid gap-4">
      <MetricChart v-for="c in charts" :key="c.title" v-bind="c" />
    </div>
  </div>
</template>
