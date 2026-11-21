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
import UFormField from '@nuxt/ui/components/FormField.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Action, Server, Volume } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import EmptyState from '@/components/EmptyState.vue'
import Panel from '@/components/Panel.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useLocations } from '@/features/servers-create/queries'
import { serverBusyKey } from '@/features/servers/shell'
import { useCurrentProject } from '@/lib/project'
import { volumeStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import { selectFullClass } from '@/lib/ui'
import CreateVolumeModal from './CreateVolumeModal.vue'
import VolumeDevice from './VolumeDevice.vue'
import { attachVolume, detachVolume, useRefreshVolumes, useVolumes, volumeDone } from './queries'
import { MAX_PER_SERVER, attachableVolumes, formatSize } from './serverPicker'

const props = defineProps<{ server: Server }>()

const toast = useToast()
const qc = useQueryClient()
const refresh = useRefreshVolumes()
const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)
const serverId = computed(() => props.server.id)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: volumes, isPending, error: loadError } = useVolumes(serverId, running)
const all = useVolumes(undefined, running)
const locations = useLocations()

const here = computed(
  () => locations.data.value?.find((l) => l.id === props.server.location.id)?.volumes_available,
)
const spare = computed(() => attachableVolumes(all.data.value ?? [], props.server))
const full = computed(() => (volumes.value?.length ?? 0) >= MAX_PER_SERVER)
const pending = ref(false)
const locked = computed(() => busy.value || running.value || pending.value)

const creating = ref(false)
const picking = ref(false)
const pick = ref<number | null>(null)
const pickError = ref<string | null>(null)
const detaching = ref<Volume | null>(null)
const detachOpen = ref(false)

function started(a: Action, name: string) {
  lastName.value = name
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void refresh()
}

async function finished(a: Action) {
  await refresh()
  const verb = volumeDone[a.command]
  if (verb) toast.add({ title: `${lastName.value} ${verb}`, color: 'success' })
}

watch(
  () => (action.value?.status === 'error' ? action.value.id : null),
  (id) => {
    if (id !== null) void refresh()
  },
)

watch(picking, (v) => {
  if (v) pick.value = spare.value[0]?.id ?? null
  pickError.value = null
})

async function attach() {
  const v = spare.value.find((x) => x.id === pick.value)
  if (!v) {
    pickError.value = 'Pick a volume.'
    return
  }
  pending.value = true
  pickError.value = null
  try {
    started(await attachVolume(v.id, props.server.id), v.name)
    picking.value = false
  } catch (e) {
    pickError.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}

function startDetach(v: Volume) {
  detaching.value = v
  detachOpen.value = true
}

async function detach() {
  const v = detaching.value
  if (!v) return
  pending.value = true
  try {
    started(await detachVolume(v.id), v.name)
    detachOpen.value = false
  } catch (e) {
    toast.add({ title: `Couldn't detach ${v.name}`, description: errorMessage(e), color: 'error' })
  } finally {
    pending.value = false
  }
}

function created(v: Volume, a: Action) {
  started(a, v.name)
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
    <p v-else-if="isPending" class="text-muted">Loading volumes…</p>
    <EmptyState
      v-else-if="!volumes?.length && here === false"
      title="Volumes aren't available here yet"
      :body="`${server.location.name} doesn't offer volumes yet. Your panel admin can set them up.`"
    />
    <EmptyState
      v-else-if="!volumes?.length"
      title="No volumes attached"
      body="A volume is extra disk space that keeps its data when you detach it or delete this server."
    >
      <div v-if="here" class="flex flex-wrap gap-2">
        <UButton icon="i-lucide-plus" :disabled="locked" @click="creating = true">
          Create volume
        </UButton>
        <UButton
          v-if="spare.length"
          color="neutral"
          variant="outline"
          :disabled="locked"
          @click="picking = true"
        >
          Attach volume
        </UButton>
      </div>
    </EmptyState>

    <template v-else>
      <div class="flex flex-wrap items-center justify-between gap-2">
        <p class="text-sm text-muted">
          {{ volumes.length }} of {{ MAX_PER_SERVER }} volumes attached.
        </p>
        <div v-if="here" class="flex flex-wrap gap-2">
          <UButton
            v-if="spare.length"
            color="neutral"
            variant="outline"
            :disabled="locked || full"
            @click="picking = true"
          >
            Attach volume
          </UButton>
          <UButton icon="i-lucide-plus" :disabled="locked || full" @click="creating = true">
            Create volume
          </UButton>
        </div>
      </div>
      <Panel v-for="v in volumes" :key="v.id" data-testid="server-volume">
        <div class="mb-4 flex flex-wrap items-center justify-between gap-2">
          <div class="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <RouterLink
              :to="{ name: 'volume', params: { projectId, volumeId: v.id } }"
              class="nb-h2 truncate text-highlighted hover:underline focus-visible:outline-2 focus-visible:outline-primary"
            >
              {{ v.name }}
            </RouterLink>
            <span class="inline-flex items-center gap-1.5 text-sm text-muted">
              <StatusDot kind="volume" :status="v.status" />{{ volumeStatus(v.status).label }}
            </span>
            <span class="text-sm text-muted tabular-nums">{{ formatSize(v.size) }}</span>
          </div>
          <UButton
            color="neutral"
            variant="outline"
            size="sm"
            :disabled="locked || v.status !== 'attached'"
            :aria-label="`Detach ${v.name}`"
            @click="startDetach(v)"
          >
            Detach
          </UButton>
        </div>
        <VolumeDevice :volume="v" hint />
      </Panel>
    </template>

    <CreateVolumeModal v-model:open="creating" :server="server" @created="created" />

    <UModal
      v-model:open="picking"
      title="Attach a volume"
      :description="`Detached volumes in ${server.location.name}.`"
    >
      <template #body>
        <form id="attach-to-server" class="flex flex-col gap-3" novalidate @submit.prevent="attach">
          <UAlert v-if="pickError" color="error" variant="subtle" :description="pickError" />
          <UFormField label="Volume" name="volume">
            <select v-model="pick" name="volume" :class="selectFullClass">
              <option v-for="v in spare" :key="v.id" :value="v.id">
                {{ v.name }} ({{ formatSize(v.size) }})
              </option>
            </select>
          </UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="picking = false">Cancel</UButton>
          <UButton type="submit" form="attach-to-server" :loading="pending">Attach</UButton>
        </div>
      </template>
    </UModal>

    <ConfirmDialog
      v-model:open="detachOpen"
      :title="`Detach ${detaching?.name ?? 'volume'}?`"
      body="Unmount it on the server first (sudo umount), or data still in memory can be lost. The volume keeps its data."
      confirm-label="Detach"
      :loading="pending"
      @confirm="detach"
    />
  </div>
</template>
