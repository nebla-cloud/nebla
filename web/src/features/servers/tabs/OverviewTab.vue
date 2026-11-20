<script setup lang="ts">
import { computed, inject, ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import ConfirmByName from '@/components/ConfirmByName.vue'
import { formatDate, typeSpec } from '@/lib/format'
import { useCurrentProject } from '@/lib/project'
import { serverBusyKey } from '../shell'

const NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [action: Action] }>()

const toast = useToast()
const qc = useQueryClient()
const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)

const rows = computed(() => {
  const s = props.server
  const list: { label: string; value: string; copy?: boolean }[] = []
  if (s.public_net.ipv4) list.push({ label: 'IPv4', value: s.public_net.ipv4.ip, copy: true })
  if (s.public_net.ipv6) list.push({ label: 'IPv6', value: s.public_net.ipv6.ip, copy: true })
  list.push(
    { label: 'Image', value: s.image.name },
    { label: 'Type', value: `${s.server_type.name}: ${typeSpec(s.server_type)}` },
    { label: 'Location', value: s.location.name },
    { label: 'Created', value: formatDate(s.created) },
  )
  return list
})

async function copy(value: string) {
  try {
    await navigator.clipboard.writeText(value)
    toast.add({ title: 'Copied', color: 'success' })
  } catch {
    toast.add({ title: "Couldn't copy", color: 'error' })
  }
}

const renaming = ref(false)
const newName = ref('')
const renameError = ref<string | null>(null)
const renameBusy = ref(false)

function startRename() {
  newName.value = props.server.name
  renameError.value = null
  renaming.value = true
}

async function submitRename() {
  const name = newName.value.trim()
  if (!NAME_PATTERN.test(name)) {
    renameError.value =
      'Use lowercase letters, numbers and hyphens, starting and ending with a letter or number.'
    return
  }
  renameBusy.value = true
  try {
    const s: Server = unwrap(
      await api.PUT('/servers/{id}', {
        params: { path: { id: props.server.id } },
        body: { name },
      }),
    )
    qc.setQueryData(['server', projectId.value, s.id], s)
    await qc.invalidateQueries({ queryKey: ['servers', projectId.value] })
    toast.add({ title: `Renamed to ${s.name}`, color: 'success' })
    renaming.value = false
  } catch (e) {
    renameError.value = errorMessage(e)
  } finally {
    renameBusy.value = false
  }
}

const confirmOpen = ref(false)
const deleting = ref(false)
const deleteError = ref<string | null>(null)

async function confirmDelete() {
  deleting.value = true
  deleteError.value = null
  try {
    const res = unwrap(
      await api.DELETE('/servers/{id}', { params: { path: { id: props.server.id } } }),
    )
    confirmOpen.value = false
    emit('started', res.action)
  } catch (e) {
    confirmOpen.value = false
    deleteError.value = errorMessage(e)
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-5">
    <form
      v-if="renaming"
      class="flex flex-wrap items-start gap-2"
      novalidate
      @submit.prevent="submitRename"
    >
      <UFormField name="name" label="Server name" :error="renameError ?? undefined">
        <UInput v-model="newName" name="name" autocomplete="off" autofocus />
      </UFormField>
      <div class="flex gap-2 sm:mt-6">
        <UButton type="submit" :loading="renameBusy">Save</UButton>
        <UButton color="neutral" variant="ghost" @click="renaming = false">Cancel</UButton>
      </div>
    </form>

    <dl class="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-2 text-sm sm:grid-cols-[7rem_1fr]">
      <dt class="text-muted">Name</dt>
      <dd class="flex flex-wrap items-center gap-2 break-all">
        {{ server.name }}
        <UButton
          v-if="!renaming"
          color="neutral"
          variant="link"
          size="xs"
          icon="i-lucide-pencil"
          @click="startRename"
        >
          Rename
        </UButton>
      </dd>
      <template v-for="r in rows" :key="r.label">
        <dt class="text-muted">{{ r.label }}</dt>
        <dd class="flex flex-wrap items-center gap-2 break-all">
          {{ r.value }}
          <UButton
            v-if="r.copy"
            color="neutral"
            variant="link"
            size="xs"
            :aria-label="`Copy ${r.label}`"
            @click="copy(r.value)"
          >
            Copy
          </UButton>
        </dd>
      </template>
    </dl>

    <UAlert v-if="deleteError" color="error" variant="subtle" :description="deleteError" />
    <div
      class="mt-2 flex flex-wrap items-center justify-between gap-3 border-t border-default pt-4"
    >
      <span class="text-sm text-muted">Delete this server and free its IP addresses.</span>
      <UButton color="error" variant="outline" :disabled="busy" @click="confirmOpen = true">
        Delete server
      </UButton>
    </div>
    <ConfirmByName
      v-model:open="confirmOpen"
      :name="server.name"
      :title="`Delete ${server.name}?`"
      confirm-label="Delete server"
      :loading="deleting"
      @confirm="confirmDelete"
    />
  </div>
</template>
