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
  <div class="flex flex-col gap-6">
    <div class="flex items-center justify-between gap-3">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">SSH keys</h1>
      <UButton v-if="keys?.length" icon="i-lucide-plus" @click="adding = true">Add key</UButton>
    </div>

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
    <ul v-else class="divide-y divide-default overflow-hidden rounded-lg border border-default">
      <li
        v-for="k in keys"
        :key="k.id"
        class="flex flex-col gap-2 px-4 py-3.5 sm:flex-row sm:items-center sm:justify-between"
      >
        <div class="flex min-w-0 flex-col gap-0.5">
          <span class="truncate font-semibold text-highlighted">{{ k.name }}</span>
          <span class="break-all font-mono text-xs text-muted">{{ k.fingerprint }}</span>
          <span class="text-xs text-muted">Added {{ formatDate(k.created) }}</span>
        </div>
        <div class="flex shrink-0 gap-1">
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
        </div>
      </li>
    </ul>

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
