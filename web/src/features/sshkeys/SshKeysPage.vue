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
import { ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import { formatDate } from '@/lib/format'
import AddSshKeyModal from './AddSshKeyModal.vue'
import { useDeleteSshKey, useRenameSshKey, useSshKeys, type SshKey } from './queries'

const toast = useToast()
const { data: keys, isPending, error: loadError } = useSshKeys()
const rename = useRenameSshKey()
const remove = useDeleteSshKey()

const adding = ref(false)
const renaming = ref<SshKey | null>(null)
const renameOpen = ref(false)
const newName = ref('')
const renameError = ref<string | null>(null)
const deleting = ref<SshKey | null>(null)
const deleteOpen = ref(false)

function startRename(k: SshKey) {
  renaming.value = k
  newName.value = k.name
  renameError.value = null
  renameOpen.value = true
}

async function submitRename() {
  const k = renaming.value
  const value = newName.value.trim()
  if (!k) return
  if (!value) {
    renameError.value = 'Give the key a name.'
    return
  }
  try {
    await rename.mutateAsync({ id: k.id, name: value })
    renameOpen.value = false
  } catch (e) {
    renameError.value = errorMessage(e)
  }
}

function startDelete(k: SshKey) {
  deleting.value = k
  deleteOpen.value = true
}

async function confirmDelete() {
  const k = deleting.value
  if (!k) return
  try {
    await remove.mutateAsync(k.id)
    toast.add({ title: `Deleted ${k.name}`, color: 'success' })
    deleteOpen.value = false
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <PageHeader title="SSH keys">
      <template v-if="keys?.length" #actions>
        <UButton icon="i-lucide-plus" @click="adding = true">Add key</UButton>
      </template>
    </PageHeader>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading keys…</p>
    <EmptyState
      v-else-if="!keys?.length"
      title="No SSH keys yet"
      body="Add a public key and you can pick it when you create a server."
    >
      <UButton icon="i-lucide-plus" @click="adding = true">Add key</UButton>
    </EmptyState>
    <ListGroup v-else>
      <ListRow v-for="k in keys" :key="k.id">
        <span class="flex min-w-0 flex-col gap-0.5 whitespace-normal">
          <span class="truncate">{{ k.name }}</span>
          <span class="nb-mono break-all text-xs font-normal text-muted">{{ k.fingerprint }}</span>
          <span class="text-xs font-normal text-muted">Added {{ formatDate(k.created) }}</span>
        </span>
        <template #trailing>
          <span class="flex gap-1">
            <UButton
              color="neutral"
              variant="ghost"
              size="sm"
              icon="i-lucide-pencil"
              :aria-label="`Rename ${k.name}`"
              @click="startRename(k)"
            >
              Rename
            </UButton>
            <UButton
              color="error"
              variant="ghost"
              size="sm"
              icon="i-lucide-trash-2"
              :aria-label="`Delete ${k.name}`"
              @click="startDelete(k)"
            >
              Delete
            </UButton>
          </span>
        </template>
      </ListRow>
    </ListGroup>

    <AddSshKeyModal v-model:open="adding" />

    <UModal v-model:open="renameOpen" title="Rename SSH key">
      <template #body>
        <form
          id="rename-ssh-key"
          class="flex flex-col gap-3"
          novalidate
          @submit.prevent="submitRename"
        >
          <UFormField label="Name" name="rename" :error="renameError ?? undefined">
            <UInput v-model="newName" name="rename" autocomplete="off" class="w-full" />
          </UFormField>
        </form>
      </template>
      <template #footer>
        <div class="flex w-full justify-end gap-2">
          <UButton color="neutral" variant="ghost" @click="renameOpen = false">Cancel</UButton>
          <UButton type="submit" form="rename-ssh-key" :loading="rename.isPending.value">
            Save
          </UButton>
        </div>
      </template>
    </UModal>

    <ConfirmByName
      v-model:open="deleteOpen"
      :name="deleting?.name ?? ''"
      :title="`Delete ${deleting?.name ?? 'key'}?`"
      confirm-label="Delete key"
      :loading="remove.isPending.value"
      @confirm="confirmDelete"
    />
  </div>
</template>
