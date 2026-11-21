<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import type { Action } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import ConfirmByName from '@/components/ConfirmByName.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useServerNames } from '@/features/servers/queries'
import { useCrumbs } from '@/lib/crumbs'
import { formatDate } from '@/lib/format'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import { isVolumeTransitional, volumeStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import AttachVolumeModal from './AttachVolumeModal.vue'
import ResizeVolumeModal from './ResizeVolumeModal.vue'
import VolumeDevice from './VolumeDevice.vue'
import { deleteVolume, detachVolume, useRefreshVolumes, useVolume, volumeDone } from './queries'
import { formatSize } from './serverPicker'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const refresh = useRefreshVolumes()
const { projectId } = useCurrentProject()
const volumeId = computed(() => parseProjectId(route.params.volumeId) ?? 0)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: volume, error: loadError } = useVolume(volumeId, running)
const attached = computed(() => volume.value?.server != null)
const { data: names } = useServerNames(attached)
const serverName = computed(() =>
  volume.value?.server != null ? (names.value?.names.get(volume.value.server) ?? null) : null,
)

useCrumbs(() => [
  { label: 'Volumes', to: { name: 'volumes', params: { projectId: projectId.value } } },
  { label: volume.value?.name ?? 'Volume' },
])

const attaching = ref(false)
const resizing = ref(false)
const detaching = ref(false)
const deleting = ref(false)
const pending = ref(false)

const locked = computed(
  () =>
    running.value || pending.value || (!!volume.value && isVolumeTransitional(volume.value.status)),
)
const status = computed(() => (volume.value ? volumeStatus(volume.value.status) : null))
const canResize = computed(
  () => volume.value?.status === 'available' || volume.value?.status === 'attached',
)
const notFound = computed(
  () => !volume.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)

function started(a: Action) {
  lastName.value = volume.value?.name ?? ''
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void refresh()
}

async function finished(a: Action) {
  const name = lastName.value || volume.value?.name || ''
  await refresh()
  if (a.command === 'delete_volume') {
    toast.add({ title: `${name} deleted`, color: 'success' })
    await router.push({ name: 'volumes', params: { projectId: projectId.value } })
    return
  }
  const verb = volumeDone[a.command]
  if (verb) toast.add({ title: `${name} ${verb}`, color: 'success' })
}

watch(
  () => (action.value?.status === 'error' ? action.value.id : null),
  (id) => {
    if (id !== null) void refresh()
  },
)

async function run(call: () => Promise<Action>, close: () => void) {
  pending.value = true
  try {
    started(await call())
    close()
  } catch (e) {
    toast.add({
      title: `Couldn't update ${volume.value?.name ?? 'the volume'}`,
      description: errorMessage(e),
      color: 'error',
    })
  } finally {
    pending.value = false
  }
}

function detach() {
  const id = volumeId.value
  void run(
    () => detachVolume(id),
    () => (detaching.value = false),
  )
}

function remove() {
  const id = volumeId.value
  void run(
    () => deleteVolume(id),
    () => (deleting.value = false),
  )
}
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-5">
    <UAlert
      v-if="notFound"
      color="error"
      variant="subtle"
      description="This volume doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !volume"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!volume" class="text-muted">Loading volume…</p>

    <template v-else>
      <PageHeader :title="volume.name">
        <template #meta>
          <span class="inline-flex items-center gap-1.5 font-medium">
            <StatusDot kind="volume" :status="volume.status" />{{ status?.label }}
          </span>
        </template>
        <template #actions>
          <UButton
            v-if="volume.status === 'available'"
            color="neutral"
            variant="outline"
            :disabled="locked"
            @click="attaching = true"
          >
            Attach
          </UButton>
          <UButton
            v-if="volume.status === 'attached'"
            color="neutral"
            variant="outline"
            :disabled="locked"
            @click="detaching = true"
          >
            Detach
          </UButton>
          <UButton
            v-if="canResize"
            color="neutral"
            variant="outline"
            :disabled="locked"
            @click="resizing = true"
          >
            Resize
          </UButton>
          <UButton
            color="error"
            variant="outline"
            :disabled="locked || attached"
            :title="attached ? 'Detach it first' : undefined"
            @click="deleting = true"
          >
            Delete
          </UButton>
        </template>
      </PageHeader>

      <Panel v-if="action" as="div">
        <ActionProgress :action="action" />
      </Panel>
      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <Panel title="Details">
        <dl class="grid grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
          <dt class="text-muted">Size</dt>
          <dd class="tabular-nums">{{ formatSize(volume.size) }}</dd>
          <dt class="text-muted">Location</dt>
          <dd>{{ volume.location.name }}</dd>
          <dt class="text-muted">Server</dt>
          <dd>
            <RouterLink
              v-if="volume.server !== null"
              :to="{ name: 'server', params: { projectId, serverId: volume.server } }"
              class="text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
            >
              {{ serverName ?? 'View server' }}
            </RouterLink>
            <span v-else class="text-muted">Not attached</span>
          </dd>
          <dt class="text-muted">Created</dt>
          <dd>{{ formatDate(volume.created) }}</dd>
        </dl>
      </Panel>

      <Panel title="Use it on the server">
        <VolumeDevice :volume="volume" hint />
      </Panel>

      <AttachVolumeModal v-model:open="attaching" :volume="volume" @started="started" />
      <ResizeVolumeModal v-model:open="resizing" :volume="volume" @started="started" />
      <ConfirmDialog
        v-model:open="detaching"
        :title="`Detach ${volume.name}?`"
        body="Unmount it on the server first, or data still in memory can be lost. The volume keeps its data."
        confirm-label="Detach"
        :loading="pending"
        @confirm="detach"
      />
      <ConfirmByName
        v-model:open="deleting"
        :name="volume.name"
        :title="`Delete ${volume.name}?`"
        confirm-label="Delete volume"
        :loading="pending"
        @confirm="remove"
      />
    </template>
  </div>
</template>
