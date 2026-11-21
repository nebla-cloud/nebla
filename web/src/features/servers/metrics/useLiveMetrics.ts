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

import { computed, type ComputedRef, type Ref } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { useCurrentProject } from '@/lib/project'
import { fetchMetrics, type TimeSeries } from './metrics'

const REFRESH_MS = 60_000
const STALE_MS = 55_000

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
    // Only the timer refreshes: no refetch on focus or remount within the interval.
    staleTime: STALE_MS,
    refetchOnWindowFocus: false,
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
