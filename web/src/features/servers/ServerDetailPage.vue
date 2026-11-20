<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import ConfirmByName from '@/components/ConfirmByName.vue'
import StatusDot from '@/components/StatusDot.vue'
import { formatDate, typeSpec } from '@/lib/format'
import { useCurrentProject, parseProjectId } from '@/lib/project'
import { serverStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import PowerActions from './PowerActions.vue'
import { useServer } from './queries'

const NAME_PATTERN = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

const done: Record<string, string> = {
  start_server: 'started',
  stop_server: 'powered off',
  shutdown_server: 'shut down',
  reboot_server: 'rebooted',
  reset_server: 'reset',
}

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const { projectId } = useCurrentProject()
const serverId = computed(() => parseProjectId(route.params.serverId) ?? 0)
const { data: server, error: loadError } = useServer(serverId)

// The create page hands its create_server action over in history state.
const handoff = router.options.history.state.actionId
const actionId = ref<number | null>(typeof handoff === 'number' ? handoff : null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })

function started(a: Action) {
  deleteError.value = null
  lastName.value = server.value?.name ?? ''
  actionId.value = a.id
}

async function finished(a: Action) {
  if (a.command === 'delete_server') {
    toast.add({ title: `${lastName.value} deleted`, color: 'success' })
    await router.push({ name: 'servers', params: { projectId: projectId.value } })
    return
  }
  const verb = done[a.command]
  if (verb) toast.add({ title: `${lastName.value} ${verb}`, color: 'success' })
}

watch(
  () => (action.value?.status === 'error' ? action.value.id : null),
  (id) => {
    if (id === null) return
    void qc.invalidateQueries({ queryKey: ['server', serverId.value] })
    void qc.invalidateQueries({ queryKey: ['servers', projectId.value] })
  },
)

const status = computed(() => (server.value ? serverStatus(server.value.status) : null))
const notFound = computed(
  () => !server.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)

const rows = computed(() => {
  const s = server.value
  if (!s) return []
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
  newName.value = server.value?.name ?? ''
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
        params: { path: { id: serverId.value } },
        body: { name },
      }),
    )
    qc.setQueryData(['server', s.id], s)
    await qc.invalidateQueries({ queryKey: ['servers'] })
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
      await api.DELETE('/servers/{id}', { params: { path: { id: serverId.value } } }),
    )
    confirmOpen.value = false
    started(res.action)
  } catch (e) {
    confirmOpen.value = false
    deleteError.value = errorMessage(e)
  } finally {
    deleting.value = false
  }
}
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-5">
    <nav class="text-sm text-muted">
      <RouterLink
        :to="{ name: 'servers', params: { projectId } }"
        class="hover:text-highlighted focus-visible:outline-2 focus-visible:outline-primary"
      >
        Servers
      </RouterLink>
      <template v-if="server">
        <span aria-hidden="true"> / </span>
        <span aria-current="page" class="text-default">{{ server.name }}</span>
      </template>
    </nav>

    <UAlert
      v-if="notFound"
      color="error"
      variant="subtle"
      description="This server doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !server"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!server" class="text-muted">Loading server…</p>

    <template v-else>
      <header class="flex flex-wrap items-center gap-x-3 gap-y-2">
        <form
          v-if="renaming"
          class="flex flex-1 flex-wrap items-start gap-2"
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
        <template v-else>
          <h1 class="text-xl font-bold tracking-tight text-highlighted">{{ server.name }}</h1>
          <UButton
            color="neutral"
            variant="ghost"
            size="sm"
            icon="i-lucide-pencil"
            @click="startRename"
          >
            Rename
          </UButton>
        </template>
        <span class="inline-flex items-center gap-1.5 font-medium">
          <StatusDot :status="server.status" />{{ status?.label }}
        </span>
        <div class="sm:ml-auto">
          <PowerActions :server="server" :busy="running" @started="started" />
        </div>
      </header>

      <div v-if="action" class="rounded-lg border border-default bg-default p-3">
        <ActionProgress :action="action" />
      </div>
      <UAlert
        v-if="actionError || deleteError"
        color="error"
        variant="subtle"
        :description="actionError ?? deleteError ?? ''"
      />

      <dl class="grid grid-cols-[6rem_1fr] gap-x-3 gap-y-2 text-sm sm:grid-cols-[7rem_1fr]">
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

      <div
        class="mt-2 flex flex-wrap items-center justify-between gap-3 border-t border-default pt-4"
      >
        <span class="text-sm text-muted">Delete this server and free its IP addresses.</span>
        <UButton color="error" variant="outline" :disabled="running" @click="confirmOpen = true">
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
    </template>
  </div>
</template>
