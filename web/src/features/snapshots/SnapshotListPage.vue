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

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Action, Snapshot } from '@/api/types'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useLocations } from '@/features/servers-create/queries'
import { useServerNames } from '@/features/servers/queries'
import { formatSize } from '@/features/volumes/serverPicker'
import { useCurrentProject } from '@/lib/project'
import { snapshotStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import { anySnapshots } from './available'
import { deletedSource } from './copy'
import { useRefreshSnapshots, useSnapshots } from './queries'
import SnapshotActions from './SnapshotActions.vue'

const toast = useToast()
const { projectId } = useCurrentProject()
const { data: snapshots, isPending, error } = useSnapshots()
const locations = useLocations()
const available = computed(() => anySnapshots(locations.data.value))
const anySource = computed(() => !!snapshots.value?.some((s) => s.server !== null))
const { data: names } = useServerNames(anySource)
const refresh = useRefreshSnapshots()
const actionId = ref<number | null>(null)
const { action } = useActionPoll(actionId)
watch(
  () => (action.value && action.value.status !== 'running' ? action.value.id : null),
  (id) => {
    if (id !== null) void refresh()
  },
)

const serverName = (s: Snapshot) =>
  s.server === null ? s.server_name : (names.value?.names.get(s.server) ?? s.server_name)

function started(s: Snapshot, a: Action) {
  toast.add({ title: `Deleting ${s.name}`, color: 'success' })
  actionId.value = a.id
  void refresh()
}
</script>

<template>
  <div class="flex flex-col gap-5">
    <PageHeader
      title="Snapshots"
      description="Saved copies of a server's boot disk. Restore the server to one, or start a new server from it."
    />

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p
      v-else-if="isPending || (locations.isPending.value && !snapshots?.length)"
      class="text-muted"
    >
      Loading snapshots…
    </p>
    <ListGroup v-else-if="snapshots?.length" label="Snapshots">
      <ListRow v-for="s in snapshots" :key="s.id">
        <template #leading><StatusDot kind="snapshot" :status="s.status" /></template>
        <span class="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <RouterLink
            :to="{ name: 'snapshot', params: { projectId, snapshotId: s.id } }"
            class="min-w-0 truncate hover:underline focus-visible:outline-2 focus-visible:outline-primary"
          >
            {{ s.name }}
          </RouterLink>
          <span
            class="flex min-w-0 flex-wrap gap-x-4 text-sm font-normal whitespace-normal text-muted"
          >
            <span class="tabular-nums">{{ formatSize(s.size) }}</span>
            <RouterLink
              v-if="s.server !== null"
              :to="{ name: 'server', params: { projectId, serverId: s.server } }"
              class="wrap-anywhere hover:underline"
            >
              {{ serverName(s) }}
            </RouterLink>
            <span v-else class="wrap-anywhere">{{ deletedSource(s.server_name) }}</span>
            <span class="hidden sm:inline">{{ s.location.name }}</span>
            <span>{{ snapshotStatus(s.status).label }}</span>
          </span>
        </span>
        <template #trailing>
          <SnapshotActions
            :snapshot="s"
            :server-name="serverName(s)"
            @started="started(s, $event)"
          />
        </template>
      </ListRow>
    </ListGroup>
    <EmptyState
      v-else-if="available === false"
      title="Snapshots aren't available here yet"
      body="None of this project's locations offer snapshots yet. Your panel admin can set them up."
    />
    <EmptyState
      v-else
      title="No snapshots yet"
      body="A snapshot saves a server's boot disk. Open a server and use its Snapshots tab to take one."
    />
  </div>
</template>
