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
import { computed, inject, ref } from 'vue'
import { RouterLink } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { NetworkGuestConfig, Server } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import Panel from '@/components/Panel.vue'
import { useLocations } from '@/features/servers-create/queries'
import { serverBusyKey } from '@/features/servers/shell'
import { useCurrentProject } from '@/lib/project'
import AttachNetworkModal from './AttachNetworkModal.vue'
import NetworkGuestDialog from './NetworkGuestDialog.vue'
import { MAX_PER_SERVER } from './choices'
import { detachFromNetwork } from './queries'
import { useNetworkOps } from './useNetworkOps'

const props = defineProps<{ server: Server }>()

const toast = useToast()
const { projectId } = useCurrentProject()
const busy = inject(
  serverBusyKey,
  computed(() => false),
)
const ops = useNetworkOps()
const locations = useLocations()

const here = computed(
  () =>
    locations.data.value?.find((l) => l.id === props.server.location.id)
      ?.private_networks_available,
)
const joined = computed(() => props.server.private_net)
const publicIps = computed(() =>
  [props.server.public_net.ipv4?.ip, props.server.public_net.ipv6?.ip].filter(Boolean).join(', '),
)
const pending = ref(false)
const locked = computed(() => busy.value || ops.running.value || pending.value)
const full = computed(() => joined.value.length >= MAX_PER_SERVER)

const attaching = ref(false)
const detaching = ref<{ network: number; name: string } | null>(null)
const detachOpen = ref(false)
const reopened = ref<NetworkGuestConfig | null>(null)
const reopenedOpen = ref(false)

function showCommands(config: NetworkGuestConfig) {
  reopened.value = config
  reopenedOpen.value = true
}

function startDetach(n: { network: number; name: string }) {
  detaching.value = n
  detachOpen.value = true
}

async function detach() {
  const d = detaching.value
  if (!d) return
  pending.value = true
  try {
    ops.started(await detachFromNetwork(props.server.id, d.network), props.server.name)
    detachOpen.value = false
  } catch (e) {
    toast.add({ title: `Couldn't detach ${d.name}`, description: errorMessage(e), color: 'error' })
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <div class="flex flex-col gap-4">
    <Panel v-if="ops.action.value" as="div">
      <ActionProgress :action="ops.action.value" />
    </Panel>
    <UAlert v-if="ops.error.value" color="error" variant="subtle" :description="ops.error.value" />

    <Panel title="Networking">
      <template v-if="here || joined.length" #header-actions>
        <UButton
          v-if="here"
          color="neutral"
          variant="outline"
          icon="i-lucide-plus"
          :disabled="locked || full"
          @click="attaching = true"
        >
          Attach to network
        </UButton>
      </template>
      <dl class="grid grid-cols-[minmax(4.5rem,8rem)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
        <dt class="text-muted">Public</dt>
        <dd class="nb-mono wrap-anywhere">
          {{ publicIps || 'No public address' }}
        </dd>
      </dl>
    </Panel>

    <Panel title="Private networks">
      <p v-if="!joined.length && here === false" class="text-sm text-muted">
        Private networks aren't available in this location.
      </p>
      <p v-else-if="!joined.length" class="text-sm text-muted">
        This server is not on a private network.
      </p>
      <table v-else class="w-full text-left text-sm">
        <thead class="text-muted">
          <tr>
            <th scope="col" class="pb-2 font-medium">Network</th>
            <th scope="col" class="pb-2 font-medium">Address</th>
            <th scope="col" class="pb-2"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-default">
          <tr v-for="n in joined" :key="n.network">
            <td class="py-2 pr-3">
              <RouterLink
                :to="{ name: 'network', params: { projectId, networkId: n.network } }"
                class="text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
              >
                {{ n.name }}
              </RouterLink>
            </td>
            <td class="nb-mono py-2 pr-3">{{ n.ip }}</td>
            <td class="flex justify-end gap-2 py-2">
              <UButton
                v-if="n.guest_config"
                color="neutral"
                variant="ghost"
                size="sm"
                icon="i-lucide-terminal"
                :aria-label="`Show commands for ${n.name}`"
                @click="showCommands(n.guest_config)"
              >
                Show commands
              </UButton>
              <UButton
                color="neutral"
                variant="outline"
                size="sm"
                :disabled="locked"
                :aria-label="`Detach ${n.name}`"
                @click="startDetach(n)"
              >
                Detach
              </UButton>
            </td>
          </tr>
        </tbody>
      </table>
    </Panel>

    <AttachNetworkModal v-model:open="attaching" :server="server" @started="ops.started" />
    <NetworkGuestDialog
      v-if="ops.guest.value"
      v-model:open="ops.showing.value"
      :config="ops.guest.value.config"
      :server-name="ops.guest.value.serverName"
    />
    <NetworkGuestDialog
      v-if="reopened"
      v-model:open="reopenedOpen"
      :config="reopened"
      :server-name="server.name"
    />
    <ConfirmDialog
      v-model:open="detachOpen"
      :title="`Detach ${detaching?.name ?? 'network'}?`"
      :body="`${server.name} leaves ${detaching?.name ?? 'the network'} and loses its address on it. Afterwards, run the commands shown.`"
      confirm-label="Detach"
      :loading="pending"
      @confirm="detach"
    />
  </div>
</template>
