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
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { errorMessage } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import Panel from '@/components/Panel.vue'
import { useLocations } from '@/features/servers-create/queries'
import { serverBusyKey } from '@/features/servers/shell'
import { formatBytes, formatDateTime } from '@/lib/format'
import { useCurrentProject } from '@/lib/project'
import { selectClass } from '@/lib/ui'
import RestoreBackupModal from './RestoreBackupModal.vue'
import {
  DEFAULT_KEEP,
  MAX_KEEP,
  MIN_KEEP,
  backUpNow,
  deleteBackup,
  isMoving,
  isPending,
  restoreBackup,
  saveBackupSettings,
  useBackups,
  useRefreshBackups,
  type Backup,
} from './queries'

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [action: Action] }>()

const qc = useQueryClient()
const refresh = useRefreshBackups()
const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)
const serverId = computed(() => props.server.id)
const { data: backups, isPending: loading, error: loadError } = useBackups(serverId)

const locations = useLocations()
// Unknown counts as available, so the toggle doesn't flicker while locations load.
const available = computed(
  () =>
    locations.data.value?.find((l) => l.id === props.server.location.id)?.backups_available ?? true,
)

const keeps = Array.from({ length: MAX_KEEP - MIN_KEEP + 1 }, (_, i) => MIN_KEEP + i)
const keep = ref(props.server.backups.keep || DEFAULT_KEEP)
watch(
  () => props.server.backups.keep,
  (k) => (keep.value = k || DEFAULT_KEEP),
)

const working = ref(false)
const error = ref<string | null>(null)
const locked = computed(() => busy.value || working.value)
const waiting = computed(() => backups.value?.some(isPending) ?? false)

// The server's last and next times move when a backup settles.
watch(
  () => backups.value?.some(isMoving) ?? false,
  (now, before) => {
    if (before && !now) void refresh()
  },
)

async function run(fn: () => Promise<void>) {
  working.value = true
  error.value = null
  try {
    await fn()
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    working.value = false
  }
}

const save = (enabled: boolean) =>
  run(async () => {
    const s = await saveBackupSettings(props.server.id, { enabled, keep: keep.value })
    qc.setQueryData(['server', projectId.value, s.id], s)
    await refresh()
  })

const now = () =>
  run(async () => {
    await backUpNow(props.server.id)
    await refresh()
  })

const label: Partial<Record<Backup['status'], string>> = {
  queued: 'Waiting for its turn',
  creating: 'Backing up…',
  restoring: 'Restoring…',
  deleting: 'Deleting…',
  missing: 'Missing',
  error: 'Failed',
}
const kindLabel: Record<Backup['kind'], string> = { scheduled: 'Scheduled', manual: 'Manual' }
const when = (b: Backup) => formatDateTime(b.completed ?? b.created)
const removable = (b: Backup) => ['available', 'error', 'missing'].includes(b.status)

const restoring = ref<Backup | null>(null)
const restoreOpen = ref(false)
const deleting = ref<Backup | null>(null)
const deleteOpen = ref(false)

function startRestore(b: Backup) {
  restoring.value = b
  error.value = null
  restoreOpen.value = true
}

function startDelete(b: Backup) {
  deleting.value = b
  error.value = null
  deleteOpen.value = true
}

const confirmRestore = () =>
  run(async () => {
    const b = restoring.value
    if (!b) return
    try {
      const a = await restoreBackup(props.server.id, b.id)
      restoreOpen.value = false
      emit('started', a)
      void refresh()
    } catch (e) {
      restoreOpen.value = false
      throw e
    }
  })

const confirmDelete = () =>
  run(async () => {
    const b = deleting.value
    if (!b) return
    try {
      const a = await deleteBackup(props.server.id, b.id)
      deleteOpen.value = false
      emit('started', a)
      void refresh()
    } catch (e) {
      deleteOpen.value = false
      throw e
    }
  })
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-4">
    <UAlert v-if="error" color="error" variant="subtle" :description="error" />

    <Panel title="Schedule">
      <div class="flex flex-col gap-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <USwitch
            :model-value="server.backups.enabled"
            label="Daily backups"
            :disabled="locked || !available"
            @update:model-value="(v: boolean) => save(v)"
          />
          <label class="flex items-center gap-2 text-sm text-muted">
            Keep
            <select
              v-model.number="keep"
              name="keep"
              :class="selectClass"
              :disabled="locked"
              @change="save(server.backups.enabled)"
            >
              <option v-for="k in keeps" :key="k" :value="k">{{ k }}</option>
            </select>
          </label>
        </div>
        <div class="flex flex-col gap-1 text-sm text-muted">
          <p v-if="server.backups.enabled && server.backups.next_backup_at">
            Next backup: {{ formatDateTime(server.backups.next_backup_at) }}
          </p>
          <p v-if="server.backups.last_backup_at">
            Last backup: {{ formatDateTime(server.backups.last_backup_at) }}
          </p>
        </div>
        <p v-if="!available" class="text-sm text-muted">
          Backups are turned off for this location. Existing backups stay restorable.
        </p>
        <div class="flex flex-wrap items-center gap-3">
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-archive"
            :disabled="locked || waiting"
            @click="now"
          >
            Back up now
          </UButton>
          <span v-if="waiting" class="text-sm text-muted">
            A backup is already waiting or running
          </span>
        </div>
      </div>
    </Panel>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="loading" class="text-muted">Loading backups…</p>
    <EmptyState
      v-else-if="!backups?.length"
      title="No backups yet"
      body="Turn on daily backups, or back up now. A backup covers the boot disk, not volumes."
    />
    <ListGroup v-else label="Backups">
      <li
        v-for="b in backups"
        :key="b.id"
        data-testid="backup"
        class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3"
      >
        <div class="flex min-w-0 flex-wrap items-baseline gap-x-3 gap-y-1">
          <time class="font-medium text-highlighted" :datetime="b.completed ?? b.created">{{
            when(b)
          }}</time>
          <span class="text-sm text-muted">{{ kindLabel[b.kind] }}</span>
          <span class="text-sm text-muted tabular-nums">{{ formatBytes(b.size) }}</span>
          <span v-if="label[b.status]" class="text-sm text-default">{{ label[b.status] }}</span>
        </div>
        <div class="flex gap-2">
          <UButton
            v-if="b.status === 'available'"
            color="neutral"
            variant="outline"
            size="sm"
            :disabled="locked"
            :aria-label="`Restore the backup from ${when(b)}`"
            @click="startRestore(b)"
          >
            Restore
          </UButton>
          <UButton
            v-if="removable(b)"
            color="neutral"
            variant="ghost"
            size="sm"
            :disabled="locked"
            :aria-label="`Delete the backup from ${when(b)}`"
            @click="startDelete(b)"
          >
            Delete
          </UButton>
        </div>
      </li>
    </ListGroup>

    <RestoreBackupModal
      v-model:open="restoreOpen"
      :name="server.name"
      :backup="restoring"
      :loading="working"
      @confirm="confirmRestore"
    />
    <ConfirmDialog
      v-model:open="deleteOpen"
      title="Delete this backup?"
      :body="deleting ? `The backup from ${when(deleting)} will be deleted. This can't be undone.` : ''"
      confirm-label="Delete backup"
      danger
      :loading="working"
      @confirm="confirmDelete"
    />
  </div>
</template>
