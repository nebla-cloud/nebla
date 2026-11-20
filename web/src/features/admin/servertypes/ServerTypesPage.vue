<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UBadge from '@nuxt/ui/components/Badge.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { ServerType } from '@/api/types'
import ConfirmByName from '@/components/ConfirmByName.vue'
import EmptyState from '@/components/EmptyState.vue'
import { useAdminLocations } from '@/lib/adminQueries'
import { typeSpec } from '@/lib/format'
import ServerTypeForm from './ServerTypeForm.vue'
import { useAdminServerTypes, useDeleteServerType } from './queries'

const toast = useToast()
const { data: types, isPending, error: loadError } = useAdminServerTypes()
const { data: locations } = useAdminLocations()
const del = useDeleteServerType()

const formOpen = ref(false)
const editing = ref<ServerType | undefined>()
const removing = ref<ServerType | null>(null)
const removeOpen = computed({
  get: () => removing.value !== null,
  set: (v) => {
    if (!v) removing.value = null
  },
})
const removeError = ref<string | null>(null)

function openForm(t?: ServerType) {
  editing.value = t
  formOpen.value = true
}

function scope(t: ServerType): string {
  if (!t.locations.length) return 'All locations'
  const names = new Map(locations.value?.map((l) => [l.id, l.name]))
  return t.locations.map((id) => names.get(id) ?? `Location ${id}`).join(', ')
}

function saved() {
  formOpen.value = false
  toast.add({ title: 'Server type saved', color: 'success' })
}

async function remove() {
  if (!removing.value) return
  removeError.value = null
  try {
    await del.mutateAsync(removing.value.id)
    toast.add({ title: `Deleted ${removing.value.name}`, color: 'success' })
    removing.value = null
  } catch (e) {
    removeError.value = errorMessage(e)
    toast.add({ title: removeError.value, color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">Server types</h1>
      <UButton icon="i-lucide-plus" @click="openForm()">New server type</UButton>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading server types…</p>
    <EmptyState
      v-else-if="!types?.length"
      title="No server types yet"
      body="A server type sets the cores, memory and disk people get when they create a server."
    >
      <UButton @click="openForm()">New server type</UButton>
    </EmptyState>
    <ul v-else class="divide-y divide-default overflow-hidden rounded-lg border border-default">
      <li
        v-for="t in types"
        :key="t.id"
        class="flex flex-wrap items-center justify-between gap-3 px-4 py-3.5"
      >
        <div class="flex min-w-0 flex-col gap-0.5">
          <div class="flex items-center gap-2">
            <span class="truncate font-semibold text-highlighted">{{ t.name }}</span>
            <UBadge v-if="t.deprecated" color="neutral" variant="subtle">Deprecated</UBadge>
          </div>
          <span class="text-sm text-muted">{{ typeSpec(t) }}</span>
          <span class="text-sm text-muted">{{ scope(t) }}</span>
        </div>
        <div class="flex gap-1">
          <UButton
            color="neutral"
            variant="ghost"
            :aria-label="`Edit ${t.name}`"
            @click="openForm(t)"
          >
            Edit
          </UButton>
          <UButton
            color="error"
            variant="ghost"
            :aria-label="`Delete ${t.name}`"
            @click="removing = t"
          >
            Delete
          </UButton>
        </div>
      </li>
    </ul>

    <UModal
      v-model:open="formOpen"
      :title="editing ? `Edit ${editing.name}` : 'New server type'"
      description="Memory is in GB; Nebla stores it in MB."
    >
      <template #body>
        <ServerTypeForm
          :key="editing?.id ?? 'new'"
          :item="editing"
          @done="saved"
          @cancel="formOpen = false"
        />
      </template>
    </UModal>

    <ConfirmByName
      v-model:open="removeOpen"
      :name="removing?.name ?? ''"
      title="Delete this server type?"
      confirm-label="Delete server type"
      :loading="del.isPending.value"
      @confirm="remove"
    />
  </div>
</template>
