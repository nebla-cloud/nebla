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
import { computed, ref, watch } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Action, FloatingIp, Server } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import { useServers } from '@/features/servers/queries'
import { selectFullClass } from '@/lib/ui'
import { assignFloatingIP } from './queries'

// At most this many floating IPs per server.
const MAX_PER_SERVER = 10

const props = defineProps<{ floatingIp: FloatingIp; action?: Action }>()
const emit = defineEmits<{ started: [action: Action] }>()
const open = defineModel<boolean>('open', { default: false })

const { data: servers, isPending } = useServers()
const moving = computed(() => props.floatingIp.server !== null)
const family = computed(() => (props.floatingIp.type === 'ipv4' ? 'IPv4' : 'IPv6'))

function reason(s: Server): string | null {
  if (s.id === props.floatingIp.server) return 'Assigned here now'
  if (s.status === 'deleting') return 'Being deleted'
  if (!s.public_net[props.floatingIp.type]) return `No ${family.value} address`
  if (s.public_net.floating_ips.length >= MAX_PER_SERVER)
    return `Already has ${MAX_PER_SERVER} floating IPs`
  return null
}

// Only servers in the floating IP's location; the API still has the final say.
const choices = computed(() =>
  (servers.value ?? [])
    .filter((s) => s.location.id === props.floatingIp.location.id)
    .map((server) => ({ server, disabledReason: reason(server) })),
)
const serverId = ref<number | null>(null)
const error = ref<string | null>(null)
const pending = ref(false)
// Only the action this open modal started.
const startedId = ref<number | null>(null)
const progress = computed(() =>
  props.action && props.action.id === startedId.value ? props.action : undefined,
)
const running = computed(() => progress.value?.status === 'running')

watch(open, (v) => {
  if (v) return
  serverId.value = null
  error.value = null
  startedId.value = null
})

async function submit() {
  error.value = null
  if (serverId.value === null) {
    error.value = 'Pick a server.'
    return
  }
  pending.value = true
  try {
    const a = await assignFloatingIP(props.floatingIp.id, serverId.value)
    startedId.value = a.id
    emit('started', a)
  } catch (e) {
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="`${moving ? 'Move' : 'Assign'} ${floatingIp.name}`"
    :description="`Servers in ${floatingIp.location.name} can use this address.`"
  >
    <template #body>
      <form id="assign-floating-ip" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <ActionProgress v-if="progress" :action="progress" />
        <p v-if="isPending" class="text-muted">Loading servers…</p>
        <p v-else-if="!choices.length" class="text-muted">
          This project has no servers in {{ floatingIp.location.name }} yet.
        </p>
        <UFormField v-else label="Server" name="server">
          <select
            v-model="serverId"
            name="server"
            :disabled="running || pending"
            :class="selectFullClass"
          >
            <option :value="null" disabled>Pick a server</option>
            <option
              v-for="c in choices"
              :key="c.server.id"
              :value="c.server.id"
              :disabled="c.disabledReason !== null"
            >
              {{ c.server.name }}{{ c.disabledReason ? ` (${c.disabledReason})` : '' }}
            </option>
          </select>
        </UFormField>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">
          {{ running ? 'Close' : 'Cancel' }}
        </UButton>
        <UButton
          type="submit"
          form="assign-floating-ip"
          :loading="pending || running"
          :disabled="running"
        >
          {{ moving ? 'Move' : 'Assign' }}
        </UButton>
      </div>
    </template>
  </UModal>
</template>
