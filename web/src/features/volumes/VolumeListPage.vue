<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { Volume } from '@/api/types'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useLocations } from '@/features/servers-create/queries'
import { useServerNames } from '@/features/servers/queries'
import { useCurrentProject } from '@/lib/project'
import { volumeStatus } from '@/lib/status'
import { anyVolumes } from './available'
import CreateVolumeModal from './CreateVolumeModal.vue'
import { useRefreshVolumes, useVolumes } from './queries'
import { formatSize } from './serverPicker'

const toast = useToast()
const { projectId } = useCurrentProject()
const { data: volumes, isPending, error } = useVolumes()
const locations = useLocations()
const available = computed(() => anyVolumes(locations.data.value))
const anyAttached = computed(() => !!volumes.value?.some((v) => v.server !== null))
const { data: names } = useServerNames(anyAttached)
const refresh = useRefreshVolumes()
const creating = ref(false)

// Attached rows name their server; the rest show their status.
function state(v: Volume): string {
  if (v.status !== 'attached' || v.server === null) return volumeStatus(v.status).label
  return names.value?.names.get(v.server) ?? 'Attached'
}

function created(v: Volume) {
  toast.add({ title: `Creating ${v.name}`, color: 'success' })
  void refresh()
}
</script>

<template>
  <div class="flex flex-col gap-5">
    <PageHeader
      title="Volumes"
      description="Extra disk space for your servers. A volume keeps its data when you detach it."
    >
      <template v-if="available && volumes?.length" #actions>
        <UButton icon="i-lucide-plus" @click="creating = true">Create volume</UButton>
      </template>
    </PageHeader>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p v-else-if="isPending || (locations.isPending.value && !volumes?.length)" class="text-muted">
      Loading volumes…
    </p>
    <ListGroup v-else-if="volumes?.length" label="Volumes">
      <ListRow
        v-for="v in volumes"
        :key="v.id"
        :to="{ name: 'volume', params: { projectId, volumeId: v.id } }"
      >
        <template #leading><StatusDot kind="volume" :status="v.status" /></template>
        {{ v.name }}
        <template #meta>
          <span class="flex flex-wrap justify-end gap-x-4">
            <span class="tabular-nums">{{ formatSize(v.size) }}</span>
            <span class="hidden sm:inline">{{ v.location.name }}</span>
            <span>{{ state(v) }}</span>
          </span>
        </template>
      </ListRow>
    </ListGroup>
    <EmptyState
      v-else-if="available === false"
      title="Volumes aren't available here yet"
      body="None of this project's locations offer volumes yet. Your panel admin can set them up."
    />
    <EmptyState
      v-else
      title="No volumes yet"
      body="A volume is extra disk space you attach to a server. It keeps its data when you detach it, rebuild the server or delete it."
    >
      <UButton icon="i-lucide-plus" @click="creating = true">Create volume</UButton>
    </EmptyState>

    <CreateVolumeModal v-model:open="creating" @created="created" />
  </div>
</template>
