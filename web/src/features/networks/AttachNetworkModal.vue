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
import UInput from '@nuxt/ui/components/Input.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { errorMessage } from '@/api/errors'
import type { Network, Server } from '@/api/types'
import { useServers } from '@/features/servers/queries'
import { selectFullClass } from '@/lib/ui'
import { networkChoices, serverChoices } from './choices'
import { attachToNetwork, useNetworks, type NetworkActionResult } from './queries'

// Set network to pick a server for it; set server to pick a network for it.
const props = defineProps<{ network?: Network; server?: Server }>()
const emit = defineEmits<{ started: [result: NetworkActionResult, serverName: string] }>()
const open = defineModel<boolean>('open', { default: false })

const { data: servers, isPending: loadingServers } = useServers()
const { data: networks, isPending: loadingNetworks } = useNetworks()

const choices = computed(() =>
  props.network
    ? serverChoices(servers.value ?? [], props.network.location.id, props.network.id).map((c) => ({
        id: c.server.id,
        label: c.server.name,
        disabledReason: c.disabledReason,
      }))
    : props.server
      ? networkChoices(networks.value ?? [], props.server).map((c) => ({
          id: c.network.id,
          label: `${c.network.name} (${c.network.ip_range})`,
          disabledReason: c.disabledReason,
        }))
      : [],
)
const picking = computed(() => (props.network ? 'server' : 'network'))
const loading = computed(() => (props.network ? loadingServers.value : loadingNetworks.value))

const picked = ref<number | null>(null)
const ip = ref('')
const error = ref<string | null>(null)
const pending = ref(false)

watch(open, (v) => {
  if (v) return
  picked.value = null
  ip.value = ''
  error.value = null
})

async function submit() {
  error.value = null
  const choice = choices.value.find((c) => c.id === picked.value)
  if (!choice) {
    error.value = `Pick a ${picking.value}.`
    return
  }
  const serverId = props.network ? choice.id : props.server!.id
  const networkId = props.network ? props.network.id : choice.id
  const serverName = props.network ? choice.label : props.server!.name
  pending.value = true
  try {
    emit('started', await attachToNetwork(serverId, networkId, ip.value.trim()), serverName)
    open.value = false
  } catch (e) {
    // The API has the final say, such as an address that's taken.
    error.value = errorMessage(e)
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <UModal
    v-model:open="open"
    :title="network ? `Attach a server to ${network.name}` : 'Attach to network'"
    :description="
      network
        ? `Servers in ${network.location.name} can join this network.`
        : `Networks in ${server?.location.name} can take this server.`
    "
  >
    <template #body>
      <form id="attach-network" class="flex flex-col gap-4" novalidate @submit.prevent="submit">
        <UAlert v-if="error" color="error" variant="subtle" :description="error" />
        <p v-if="loading" class="text-muted">Loading…</p>
        <p v-else-if="!choices.length" class="text-muted">
          {{
            network
              ? 'No servers in this location yet.'
              : 'No networks in this location yet. Create one on the Networks page.'
          }}
        </p>
        <template v-else>
          <UFormField :label="network ? 'Server' : 'Network'" :name="picking">
            <select v-model="picked" :name="picking" :class="selectFullClass">
              <option :value="null" disabled>Pick a {{ picking }}</option>
              <option
                v-for="c in choices"
                :key="c.id"
                :value="c.id"
                :disabled="c.disabledReason !== null"
              >
                {{ c.label }}{{ c.disabledReason ? ` (${c.disabledReason})` : '' }}
              </option>
            </select>
          </UFormField>
          <UFormField
            label="Address"
            name="ip"
            hint="Optional"
            help="Left empty, the lowest free address is used."
          >
            <UInput
              v-model="ip"
              name="ip"
              autocomplete="off"
              spellcheck="false"
              placeholder="10.0.0.10"
              class="w-full"
            />
          </UFormField>
        </template>
      </form>
    </template>
    <template #footer>
      <div class="flex w-full justify-end gap-2">
        <UButton color="neutral" variant="ghost" @click="open = false">Cancel</UButton>
        <UButton type="submit" form="attach-network" :loading="pending">Attach</UButton>
      </div>
    </template>
  </UModal>
</template>
