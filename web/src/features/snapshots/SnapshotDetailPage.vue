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
import { computed, ref } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import type { Action } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import StatusDot from '@/components/StatusDot.vue'
import ActionList from '@/features/activity/ActionList.vue'
import { useServerNames } from '@/features/servers/queries'
import { formatSize } from '@/features/volumes/serverPicker'
import { useCrumbs } from '@/lib/crumbs'
import { formatDate } from '@/lib/format'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import { snapshotStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import { deletedSource } from './copy'
import { useRefreshSnapshots, useSnapshot, useSnapshotActions } from './queries'
import SnapshotActions from './SnapshotActions.vue'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const refresh = useRefreshSnapshots()
const { projectId } = useCurrentProject()
const snapshotId = computed(() => parseProjectId(route.params.snapshotId) ?? 0)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: snapshot, error: loadError } = useSnapshot(snapshotId, running)
const { data: actions } = useSnapshotActions(snapshotId, running)
const hasSource = computed(() => snapshot.value?.server != null)
const { data: names } = useServerNames(hasSource)
const serverName = computed(() => {
  const s = snapshot.value
  if (!s) return ''
  return s.server === null ? s.server_name : (names.value?.names.get(s.server) ?? s.server_name)
})

useCrumbs(() => [
  { label: 'Snapshots', to: { name: 'snapshots', params: { projectId: projectId.value } } },
  { label: snapshot.value?.name ?? 'Snapshot' },
])

const status = computed(() => (snapshot.value ? snapshotStatus(snapshot.value.status) : null))
const notFound = computed(
  () => !snapshot.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)

function started(a: Action) {
  lastName.value = snapshot.value?.name ?? ''
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void refresh()
}

async function finished(a: Action) {
  await refresh()
  if (a.command === 'delete_snapshot') {
    toast.add({ title: `${lastName.value} deleted`, color: 'success' })
    await router.push({ name: 'snapshots', params: { projectId: projectId.value } })
  }
}
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-5">
    <UAlert
      v-if="notFound"
      color="error"
      variant="subtle"
      description="This snapshot doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !snapshot"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!snapshot" class="text-muted">Loading snapshot…</p>

    <template v-else>
      <PageHeader :title="snapshot.name" class="max-sm:flex-col max-sm:items-stretch">
        <template #meta>
          <span class="inline-flex items-center gap-1.5 font-medium">
            <StatusDot kind="snapshot" :status="snapshot.status" />{{ status?.label }}
          </span>
        </template>
        <template #actions>
          <SnapshotActions
            expanded
            :snapshot="snapshot"
            :server-name="serverName"
            :disabled="running"
            @started="started"
          />
        </template>
      </PageHeader>

      <Panel v-if="action" as="div">
        <ActionProgress :action="action" />
      </Panel>
      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <Panel title="Details">
        <dl class="grid grid-cols-[minmax(4.5rem,8rem)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
          <template v-if="snapshot.description">
            <dt class="text-muted">Description</dt>
            <dd class="wrap-anywhere">{{ snapshot.description }}</dd>
          </template>
          <dt class="text-muted">Size</dt>
          <dd class="tabular-nums">{{ formatSize(snapshot.size) }}</dd>
          <dt class="text-muted">Location</dt>
          <dd class="wrap-anywhere">{{ snapshot.location.name }}</dd>
          <dt class="text-muted">Source server</dt>
          <dd class="wrap-anywhere">
            <RouterLink
              v-if="snapshot.server !== null"
              :to="{ name: 'server', params: { projectId, serverId: snapshot.server } }"
              class="text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
            >
              {{ serverName }}
            </RouterLink>
            <span v-else class="text-muted">{{ deletedSource(snapshot.server_name) }}</span>
          </dd>
          <template v-if="snapshot.image">
            <dt class="text-muted">Image</dt>
            <dd class="wrap-anywhere">{{ snapshot.image.name }}</dd>
          </template>
          <dt class="text-muted">Created</dt>
          <dd>{{ formatDate(snapshot.created) }}</dd>
        </dl>
      </Panel>

      <Panel title="Activity">
        <p v-if="!actions?.length" class="text-muted">Nothing has happened here yet.</p>
        <ActionList v-else :actions="actions" :show-resource="false" />
      </Panel>
    </template>
  </div>
</template>
