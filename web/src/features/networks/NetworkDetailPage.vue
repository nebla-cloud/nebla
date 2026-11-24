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
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import ActionProgress from '@/components/ActionProgress.vue'
import ConfirmByName from '@/components/ConfirmByName.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import LabelChip from '@/components/LabelChip.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import { useCrumbs } from '@/lib/crumbs'
import { formatDate } from '@/lib/format'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import AttachNetworkModal from './AttachNetworkModal.vue'
import NetworkGuestDialog from './NetworkGuestDialog.vue'
import { deleteNetwork, detachFromNetwork, useNetwork, useRefreshNetworks } from './queries'
import { useNetworkOps } from './useNetworkOps'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const refresh = useRefreshNetworks()
const { projectId } = useCurrentProject()
const networkId = computed(() => parseProjectId(route.params.networkId) ?? 0)
const { data: net, error: loadError } = useNetwork(networkId)
const ops = useNetworkOps()

useCrumbs(() => [
  { label: 'Networks', to: { name: 'networks', params: { projectId: projectId.value } } },
  { label: net.value?.name ?? 'Network' },
])

const attaching = ref(false)
const deleting = ref(false)
const detachTarget = ref<{ server: number; name: string } | null>(null)
const detachOpen = ref(false)
const pending = ref(false)

const attached = computed(() => net.value?.servers ?? [])
const locked = computed(() => ops.running.value || pending.value)
const labels = computed(() => Object.entries(net.value?.labels ?? {}))
const notFound = computed(
  () => !net.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)

function startDetach(s: { server: number; name: string }) {
  detachTarget.value = s
  detachOpen.value = true
}

async function detach() {
  const t = detachTarget.value
  if (!t || !net.value) return
  pending.value = true
  try {
    ops.started(await detachFromNetwork(t.server, net.value.id), t.name)
    detachOpen.value = false
  } catch (e) {
    toast.add({ title: `Couldn't detach ${t.name}`, description: errorMessage(e), color: 'error' })
  } finally {
    pending.value = false
  }
}

async function remove() {
  const n = net.value
  if (!n) return
  pending.value = true
  try {
    await deleteNetwork(n.id)
    deleting.value = false
    await refresh()
    toast.add({ title: `${n.name} deleted`, color: 'success' })
    await router.push({ name: 'networks', params: { projectId: projectId.value } })
  } catch (e) {
    deleting.value = false
    toast.add({ title: `Couldn't delete ${n.name}`, description: errorMessage(e), color: 'error' })
  } finally {
    pending.value = false
  }
}
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-5">
    <UAlert
      v-if="notFound"
      color="error"
      variant="subtle"
      description="This network doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !net"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!net" class="text-muted">Loading network…</p>

    <template v-else>
      <PageHeader :title="net.name" class="max-sm:flex-col max-sm:items-stretch">
        <template #actions>
          <span :title="attached.length ? 'Detach all servers first.' : undefined">
            <UButton
              color="error"
              variant="outline"
              :disabled="attached.length > 0 || locked"
              @click="deleting = true"
            >
              Delete
            </UButton>
          </span>
        </template>
      </PageHeader>

      <Panel v-if="ops.action.value" as="div">
        <ActionProgress :action="ops.action.value" />
      </Panel>
      <UAlert
        v-if="ops.error.value"
        color="error"
        variant="subtle"
        :description="ops.error.value"
      />

      <Panel title="Details">
        <dl class="grid grid-cols-[minmax(4.5rem,8rem)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
          <dt class="text-muted">Range</dt>
          <dd class="nb-mono wrap-anywhere">{{ net.ip_range }}</dd>
          <dt class="text-muted">Location</dt>
          <dd class="wrap-anywhere">{{ net.location.name }}</dd>
          <dt class="text-muted">Labels</dt>
          <dd class="flex flex-wrap gap-1.5">
            <LabelChip v-for="[k, v] in labels" :key="k" :k="k" :v="v" />
            <span v-if="!labels.length" class="text-muted">None</span>
          </dd>
          <dt class="text-muted">Created</dt>
          <dd>{{ formatDate(net.created) }}</dd>
        </dl>
      </Panel>

      <Panel title="Servers">
        <template #header-actions>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-plus"
            :disabled="locked"
            @click="attaching = true"
          >
            Attach server
          </UButton>
        </template>
        <p v-if="!attached.length" class="text-sm text-muted">No servers on this network yet.</p>
        <table v-else class="w-full text-left text-sm">
          <thead class="text-muted">
            <tr>
              <th scope="col" class="pb-2 font-medium">Server</th>
              <th scope="col" class="pb-2 font-medium">Address</th>
              <th scope="col" class="pb-2"><span class="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody class="divide-y divide-default">
            <tr v-for="s in attached" :key="s.server">
              <td class="py-2 pr-3">
                <RouterLink
                  :to="{ name: 'server', params: { projectId, serverId: s.server } }"
                  class="text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
                >
                  {{ s.name }}
                </RouterLink>
              </td>
              <td class="nb-mono py-2 pr-3">{{ s.ip }}</td>
              <td class="py-2 text-right">
                <UButton
                  color="neutral"
                  variant="outline"
                  size="sm"
                  :disabled="locked"
                  :aria-label="`Detach ${s.name}`"
                  @click="startDetach(s)"
                >
                  Detach
                </UButton>
              </td>
            </tr>
          </tbody>
        </table>
      </Panel>

      <AttachNetworkModal v-model:open="attaching" :network="net" @started="ops.started" />
      <NetworkGuestDialog
        v-if="ops.guest.value"
        v-model:open="ops.showing.value"
        :config="ops.guest.value.config"
        :server-name="ops.guest.value.serverName"
      />
      <ConfirmDialog
        v-model:open="detachOpen"
        :title="`Detach ${detachTarget?.name ?? 'server'}?`"
        :body="`${detachTarget?.name ?? 'The server'} leaves ${net.name} and loses its address on it. Afterwards, run the commands shown.`"
        confirm-label="Detach"
        :loading="pending"
        @confirm="detach"
      />
      <ConfirmByName
        v-model:open="deleting"
        :name="net.name"
        :title="`Delete ${net.name}?`"
        confirm-label="Delete network"
        :loading="pending"
        @confirm="remove"
      />
    </template>
  </div>
</template>
