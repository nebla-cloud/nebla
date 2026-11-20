<script setup lang="ts">
import { computed, inject, ref, watch } from 'vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { api } from '@/api/client'
import { errorMessage, unwrap } from '@/api/errors'
import type { Action, Server } from '@/api/types'
import { useServerTypes } from '@/features/servers-create/queries'
import { RouterLink } from 'vue-router'
import { useCurrentProject } from '@/lib/project'
import { typeSpec } from '@/lib/format'
import { typeChoices } from '../resize'
import { serverBusyKey } from '../shell'

const props = defineProps<{ server: Server }>()
const emit = defineEmits<{ started: [action: Action] }>()

const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)
const types = useServerTypes()
const grow = ref(false)
const picked = ref<number | null>(null)
const pending = ref(false)
const error = ref('')

const choices = computed(() => typeChoices(types.data.value ?? [], props.server, grow.value))
const isOff = computed(() => props.server.status === 'off')
const note = computed(() => {
  switch (props.server.status) {
    case 'running':
      return 'Shut the server down to resize it.'
    case 'starting':
      return "The server is starting. Resizing unlocks once it's off."
    case 'stopping':
      return "The server is stopping. Resizing unlocks once it's off."
    case 'error':
      return "Fix the server's state from Overview, then come back to resize."
    case 'missing':
      return "Nebla can't find this server right now. Check Overview, then come back to resize."
    default:
      return 'Resizing unlocks once the server is off.'
  }
})
const canSubmit = computed(
  () => isOff.value && picked.value !== null && !busy.value && !pending.value,
)

// Drop a pick that turned invalid, such as after ticking grow disk.
watch(choices, (list) => {
  const c = list.find((x) => x.type.id === picked.value)
  if (c?.disabledReason) picked.value = null
})

async function run(call: () => Promise<{ data?: unknown; error?: unknown; response: Response }>) {
  pending.value = true
  error.value = ''
  try {
    emit(
      'started',
      unwrap((await call()) as { data?: { action: Action }; response: Response }).action,
    )
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}

function resize() {
  if (picked.value === null) return
  void run(() =>
    api.POST('/servers/{id}/actions/change_type', {
      params: { path: { id: props.server.id } },
      body: { server_type: String(picked.value), upgrade_disk: grow.value },
    }),
  )
}

function shutdown() {
  void run(() =>
    api.POST('/servers/{id}/actions/shutdown', { params: { path: { id: props.server.id } } }),
  )
}
</script>

<template>
  <div class="flex max-w-2xl flex-col gap-4">
    <div
      v-if="!isOff"
      class="flex flex-wrap items-center justify-between gap-3 rounded-md border border-default p-3 text-sm"
    >
      <span>{{ note }}</span>
      <UButton
        v-if="server.status === 'running'"
        color="neutral"
        variant="outline"
        :disabled="busy || pending"
        @click="shutdown"
      >
        Shut down
      </UButton>
      <RouterLink
        v-else
        :to="{ name: 'server', params: { projectId, serverId: server.id } }"
        class="text-primary underline"
      >
        Go to Overview
      </RouterLink>
    </div>
    <p class="text-sm text-muted">Now {{ typeSpec(server.server_type) }}.</p>
    <label class="flex items-center gap-2 text-sm">
      <input v-model="grow" type="checkbox" name="upgrade_disk" />
      Grow the disk too
    </label>
    <p class="text-sm text-muted">
      Disks never shrink. If you grow it, you can't move back to a type with a smaller disk.
    </p>
    <fieldset class="flex flex-col gap-2">
      <legend class="mb-1 text-sm font-medium">Type</legend>
      <label
        v-for="c in choices"
        :key="c.type.id"
        class="flex items-start gap-2 rounded-md border border-default p-3 text-sm"
        :class="c.disabledReason ? 'opacity-60' : ''"
      >
        <input
          v-model="picked"
          type="radio"
          name="type"
          :value="c.type.id"
          :disabled="!!c.disabledReason"
          class="mt-1"
        />
        <span class="flex flex-col">
          <span>{{ c.type.name }}, {{ typeSpec(c.type) }}</span>
          <span class="text-muted">{{ c.disabledReason ?? c.delta }}</span>
        </span>
      </label>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-error">{{ error }}</p>
    <div>
      <UButton :disabled="!canSubmit" :loading="pending" @click="resize">Resize</UButton>
    </div>
  </div>
</template>
