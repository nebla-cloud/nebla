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
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UDropdownMenu from '@nuxt/ui/components/DropdownMenu.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { errorMessage } from '@/api/errors'
import type { Action, Snapshot } from '@/api/types'
import ProtectionButton from '@/components/ProtectionButton.vue'
import ConfirmByName from '@/components/ConfirmByName.vue'
import { useCurrentProject } from '@/lib/project'
import { isSnapshotTransitional } from '@/lib/status'
import RestoreSnapshotModal from './RestoreSnapshotModal.vue'
import { deleteSnapshot, renameSnapshot, updateSnapshot, useRefreshSnapshots } from './queries'

// Restore, New server, Rename and Delete for one snapshot; expanded shows them as buttons.
const props = defineProps<{
  snapshot: Snapshot
  serverName: string
  expanded?: boolean
  // On the source server's own page the parent tracks the restore instead.
  stay?: boolean
  disabled?: boolean
}>()
// started fires for a delete; a restore carries on at the server's page, or fires restoring when stay is set.
const emit = defineEmits<{ started: [action: Action]; restoring: [action: Action] }>()

const toast = useToast()
const router = useRouter()
const qc = useQueryClient()
const { projectId } = useCurrentProject()
const refresh = useRefreshSnapshots()

const protectionFlags = [
  {
    key: 'delete' as const,
    label: 'Delete',
    description: "Blocks deleting this snapshot until it's turned off.",
  },
]

const ready = computed(() => props.snapshot.status === 'available')
const locked = computed(() => props.disabled || isSnapshotTransitional(props.snapshot.status))
const canRestore = computed(() => props.snapshot.server !== null && ready.value)

const restoring = ref(false)
const renaming = ref(false)
const deleting = ref(false)
const newName = ref('')
const renameError = ref<string | null>(null)
const pending = ref(false)

async function restored(a: Action) {
  if (props.stay) {
    emit('restoring', a)
    return
  }
  const serverId = props.snapshot.server
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['servers', projectId.value] })
  if (serverId === null) return
  await router.push({
    name: 'server',
    params: { projectId: projectId.value, serverId },
    state: { actionId: a.id },
  })
}

function newServer(): void {
  void router.push({
    name: 'server-new',
    params: { projectId: projectId.value },
    query: { snapshot: String(props.snapshot.id) },
  })
}

function startRename() {
  newName.value = props.snapshot.name
  renameError.value = null
  renaming.value = true
}

async function submitRename() {
  const name = newName.value.trim()
  if (!/^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(name)) {
    renameError.value =
      'Use lowercase letters, numbers and hyphens, starting and ending with a letter or number.'
    return
  }
  pending.value = true
  try {
    await renameSnapshot(props.snapshot.id, name)
    renaming.value = false
    await refresh()
  } catch (e) {
    renameError.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}

async function remove() {
  pending.value = true
  try {
    emit('started', await deleteSnapshot(props.snapshot.id))
    deleting.value = false
  } catch (e) {
    toast.add({
      title: `Couldn't delete ${props.snapshot.name}`,
      description: errorMessage(e),
      color: 'error',
    })
  } finally {
    pending.value = false
  }
}

const menu = computed(() => [
  [
    { label: 'New server', icon: 'i-lucide-plus', disabled: !ready.value, onSelect: newServer },
    { label: 'Rename', icon: 'i-lucide-pencil', onSelect: startRename },
  ],
  [
    {
      label: 'Delete',
      icon: props.snapshot.protection.delete ? 'i-lucide-lock' : 'i-lucide-trash-2',
      color: 'error' as const,
      disabled: props.snapshot.protection.delete,
      onSelect: (): void => {
        deleting.value = true
      },
    },
  ],
])
</script>

<template>
  <div class="flex flex-wrap items-center gap-2">
    <UButton
      v-if="canRestore"
      color="neutral"
      variant="outline"
      :size="expanded ? 'md' : 'sm'"
      :disabled="locked"
      :aria-label="`Restore ${snapshot.name}`"
      @click="restoring = true"
    >
      Restore
    </UButton>
    <template v-if="expanded">
      <UButton color="neutral" variant="outline" :disabled="locked || !ready" @click="newServer">
        New server
      </UButton>
      <UButton color="neutral" variant="outline" :disabled="locked" @click="startRename">
        Rename
      </UButton>
      <ProtectionButton
        :flags="protectionFlags"
        :value="snapshot.protection"
        :save="(c) => updateSnapshot(snapshot.id, { protection: { delete: c.delete } })"
        @saved="refresh"
      />
      <UTooltip
        :text="snapshot.protection.delete ? 'Turn off delete protection first.' : undefined"
        :disabled="!snapshot.protection.delete"
      >
        <UButton
          color="error"
          variant="outline"
          :icon="snapshot.protection.delete ? 'i-lucide-lock' : undefined"
          :disabled="locked || snapshot.protection.delete"
          @click="deleting = true"
        >
          Delete
        </UButton>
      </UTooltip>
    </template>
    <UDropdownMenu v-else :items="menu" :content="{ align: 'end' }" :disabled="locked">
      <UButton
        color="neutral"
        variant="ghost"
        size="sm"
        icon="i-lucide-ellipsis"
        :aria-label="`More actions for ${snapshot.name}`"
        :disabled="locked"
      />
    </UDropdownMenu>

    <RestoreSnapshotModal
      v-model:open="restoring"
      :snapshot="snapshot"
      :server-name="serverName"
      @started="restored"
    />
    <UModal v-model:open="renaming" :title="`Rename ${snapshot.name}`">
      <template #body>
        <form
          id="rename-snapshot"
          class="flex flex-col gap-3"
          novalidate
          @submit.prevent="submitRename"
        >
          <UAlert v-if="renameError" color="error" variant="subtle" :description="renameError" />
          <UFormField label="Name" name="name">
            <UInput v-model="newName" name="name" autocomplete="off" class="w-full" />
          </UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="renaming = false">Cancel</UButton>
          <UButton type="submit" form="rename-snapshot" :loading="pending">Rename</UButton>
        </div>
      </template>
    </UModal>
    <ConfirmByName
      v-model:open="deleting"
      :name="snapshot.name"
      :title="`Delete ${snapshot.name}?`"
      confirm-label="Delete snapshot"
      :loading="pending"
      @confirm="remove"
    />
  </div>
</template>
