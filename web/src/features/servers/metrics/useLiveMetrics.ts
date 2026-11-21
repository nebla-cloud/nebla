import { computed, type ComputedRef, type Ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { useCurrentProject } from '@/lib/project'
import { fetchMetrics, type TimeSeries } from './metrics'

const REFRESH_MS = 60_000

function latest(points: [number, number | null][] | undefined): number | null {
  if (!points) return null
  for (let i = points.length - 1; i >= 0; i--) {
    const v = points[i]![1]
    if (v != null) return v
  }
  return null
}

export function useLiveMetrics(
  serverId: Ref<number>,
  enabled: Ref<boolean>,
): {
  cpu: ComputedRef<number | null>
  netIn: ComputedRef<number | null>
  netOut: ComputedRef<number | null>
  diskRead: ComputedRef<number | null>
  diskWrite: ComputedRef<number | null>
  series: ComputedRef<TimeSeries>
} {
  const { projectId } = useCurrentProject()
  // Same prefix as GraphsTab, with a 'live' suffix so the two caches stay apart.
  const query = useQuery({
    queryKey: computed(() => ['server-metrics', projectId.value, serverId.value, 'hour', 'live']),
    queryFn: () => fetchMetrics(serverId.value, 'hour'),
    enabled: computed(() => enabled.value && projectId.value > 0 && serverId.value > 0),
    retry: false,
    refetchInterval: REFRESH_MS,
    refetchIntervalInBackground: false,
  })

  const series = computed(() => (query.data.value?.time_series ?? {}) as TimeSeries)
  const pick = (key: string) => computed(() => latest(series.value[key]))
  return {
    cpu: pick('cpu'),
    netIn: pick('network.in'),
    netOut: pick('network.out'),
    diskRead: pick('disk.read'),
    diskWrite: pick('disk.write'),
    series,
  }
}
