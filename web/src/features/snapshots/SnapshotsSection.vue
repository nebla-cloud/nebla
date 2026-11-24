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
import { computed, inject, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Action, Server, Snapshot } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import EmptyState from '@/components/EmptyState.vue'
import Panel from '@/components/Panel.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useLocations } from '@/features/servers-create/queries'
import { serverBusyKey } from '@/features/servers/shell'
import { formatSize } from '@/features/volumes/serverPicker'
import { formatDate } from '@/lib/format'
import { useCurrentProject } from '@/lib/project'
import { snapshotStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import { useRefreshSnapshots, useSnapshots } from './queries'
import SnapshotActions from './SnapshotActions.vue'
import TakeSnapshotModal from './TakeSnapshotModal.vue'

const props = defineProps<{ server: Server }>()

const toast = useToast()
const qc = useQueryClient()
const refresh = useRefreshSnapshots()
const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)
const serverId = computed(() => props.server.id)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: snapshots, isPending, error: loadError } = useSnapshots(serverId, running)
const locations = useLocations()

const here = computed(
  () => locations.data.value?.find((l) => l.id === props.server.location.id)?.snapshots_available,
)
const taking = ref(false)
const locked = computed(() => busy.value || running.value)

function started(a: Action, name: string) {
  lastName.value = name
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void refresh()
}

async function finished(a: Action) {
  await refresh()
  if (a.command === 'create_snapshot')
    toast.add({ title: `${lastName.value} saved`, color: 'success' })
  if (a.command === 'delete_snapshot')
    toast.add({ title: `${lastName.value} deleted`, color: 'success' })
}

watch(
  () => (action.value?.status === 'error' ? action.value.id : null),
  (id) => {
    if (id !== null) void refresh()
  },
)

function taken(s: Snapshot, a: Action) {
  started(a, s.name)
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <Panel v-if="action" as="div">
      <ActionProgress :action="action" />
    </Panel>
    <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading snapshots…</p>
    <EmptyState
      v-else-if="!snapshots?.length && here === false"
      title="Snapshots aren't available here yet"
      :body="`${server.location.name} doesn't offer snapshots yet. Your panel admin can set them up.`"
    />
    <EmptyState
      v-else-if="!snapshots?.length"
      title="No snapshots of this server"
      body="A snapshot saves the boot disk. Attached volumes aren't included."
    >
      <UButton v-if="here" icon="i-lucide-camera" :disabled="locked" @click="taking = true">
        Take snapshot
      </UButton>
    </EmptyState>

    <template v-else>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="text-sm text-muted">Saves the boot disk. Attached volumes aren't included.</p>
        <UButton v-if="here" icon="i-lucide-camera" :disabled="locked" @click="taking = true">
          Take snapshot
        </UButton>
      </div>
      <Panel v-for="s in snapshots" :key="s.id" data-testid="server-snapshot">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <RouterLink
              :to="{ name: 'snapshot', params: { projectId, snapshotId: s.id } }"
              class="nb-h2 truncate text-highlighted hover:underline focus-visible:outline-2 focus-visible:outline-primary"
            >
              {{ s.name }}
            </RouterLink>
            <span class="inline-flex items-center gap-1.5 text-sm text-muted">
              <StatusDot kind="snapshot" :status="s.status" />{{ snapshotStatus(s.status).label }}
            </span>
            <span class="text-sm text-muted tabular-nums">{{ formatSize(s.size) }}</span>
            <span class="text-sm text-muted">{{ formatDate(s.created) }}</span>
          </div>
          <SnapshotActions
            :snapshot="s"
            :server-name="server.name"
            :disabled="locked"
            @started="started($event, s.name)"
          />
        </div>
      </Panel>
    </template>

    <TakeSnapshotModal v-model:open="taking" :server="server" @started="taken" />
  </div>
</template>
