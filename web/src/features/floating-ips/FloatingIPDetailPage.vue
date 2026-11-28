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
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { ApiError, errorMessage } from '@/api/errors'
import type { Action } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import ProtectionButton from '@/components/ProtectionButton.vue'
import ConfirmByName from '@/components/ConfirmByName.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useServerNames } from '@/features/servers/queries'
import { useCrumbs } from '@/lib/crumbs'
import { formatDate } from '@/lib/format'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import { floatingIpStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import AssignFloatingIPModal from './AssignFloatingIPModal.vue'
import GuestConfigDialog from './GuestConfigDialog.vue'
import {
  deleteFloatingIP,
  floatingIPDone,
  unassignFloatingIP,
  updateFloatingIP,
  useFloatingIP,
  useRefreshFloatingIPs,
} from './queries'

const RELEASING = 'The address is still being released from the previous server.'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const refresh = useRefreshFloatingIPs()
const { projectId } = useCurrentProject()
const floatingIpId = computed(() => parseProjectId(route.params.floatingIpId) ?? 0)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: fip, error: loadError } = useFloatingIP(floatingIpId, running)
const assigned = computed(() => fip.value?.server != null)
const { data: names } = useServerNames(assigned)
const serverName = computed(() =>
  fip.value?.server != null ? (names.value?.names.get(fip.value.server) ?? null) : null,
)

useCrumbs(() => [
  { label: 'Floating IPs', to: { name: 'floating-ips', params: { projectId: projectId.value } } },
  { label: fip.value?.name ?? 'Floating IP' },
])

const protectionFlags = [
  {
    key: 'delete' as const,
    label: 'Delete',
    description: "Blocks deleting this floating IP until it's turned off.",
  },
]

const assigning = ref(false)
const unassigning = ref(false)
const deleting = ref(false)
const showingCommands = ref(false)
const pending = ref(false)

const locked = computed(
  () => running.value || pending.value || (!!fip.value && fip.value.status !== 'active'),
)
const status = computed(() => (fip.value ? floatingIpStatus(fip.value.status) : null))
const assignAction = computed(() =>
  action.value?.command === 'assign_floating_ip' ? action.value : undefined,
)
const notFound = computed(
  () => !fip.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)

function started(a: Action) {
  lastName.value = fip.value?.name ?? ''
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void refresh()
}

async function finished(a: Action) {
  const name = lastName.value || fip.value?.name || ''
  if (a.command === 'delete_floating_ip') {
    await refresh()
    toast.add({ title: `${name} deleted`, color: 'success' })
    await router.push({ name: 'floating-ips', params: { projectId: projectId.value } })
    return
  }
  await refresh()
  const verb = floatingIPDone[a.command]
  if (verb) toast.add({ title: `${name} ${verb}`, color: 'success' })
  assigning.value = false
  if (fip.value?.guest_config) showingCommands.value = true
}

watch(
  () => (action.value?.status === 'error' ? action.value.id : null),
  (id) => {
    if (id !== null) void refresh()
  },
)

async function run(call: () => Promise<Action>, close: () => void) {
  pending.value = true
  try {
    started(await call())
    close()
  } catch (e) {
    toast.add({
      title: `Couldn't update ${fip.value?.name ?? 'the floating IP'}`,
      description: errorMessage(e),
      color: 'error',
    })
  } finally {
    pending.value = false
  }
}

function unassign() {
  const id = floatingIpId.value
  void run(
    () => unassignFloatingIP(id),
    () => (unassigning.value = false),
  )
}

function remove() {
  const id = floatingIpId.value
  void run(
    () => deleteFloatingIP(id),
    () => (deleting.value = false),
  )
}
</script>

<template>
  <div class="flex max-w-3xl flex-col gap-5">
    <UAlert
      v-if="notFound"
      color="error"
      variant="subtle"
      description="This floating IP doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !fip"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!fip" class="text-muted">Loading floating IP…</p>

    <template v-else>
      <PageHeader :title="fip.name" class="max-sm:flex-col max-sm:items-stretch">
        <template #meta>
          <span class="inline-flex items-center gap-1.5 font-medium">
            <StatusDot kind="floating-ip" :status="fip.status" />{{ status?.label }}
          </span>
        </template>
        <template #actions>
          <UButton
            v-if="fip.guest_config"
            color="neutral"
            variant="outline"
            icon="i-lucide-terminal"
            @click="showingCommands = true"
          >
            Show commands
          </UButton>
          <UButton
            v-if="!assigned"
            color="neutral"
            variant="outline"
            :disabled="locked"
            @click="assigning = true"
          >
            Assign
          </UButton>
          <template v-else>
            <UButton color="neutral" variant="outline" :disabled="locked" @click="assigning = true">
              Move
            </UButton>
            <UButton
              color="neutral"
              variant="outline"
              :disabled="locked"
              @click="unassigning = true"
            >
              Unassign
            </UButton>
          </template>
          <ProtectionButton
            :flags="protectionFlags"
            :value="fip.protection"
            :save="(c) => updateFloatingIP(floatingIpId, { protection: { delete: c.delete } })"
            @saved="refresh"
          />
          <UTooltip
            :text="fip.protection.delete ? 'Turn off delete protection first.' : undefined"
            :disabled="!fip.protection.delete"
          >
            <UButton
              color="error"
              variant="outline"
              :icon="fip.protection.delete ? 'i-lucide-lock' : undefined"
              :disabled="locked || fip.protection.delete"
              @click="deleting = true"
            >
              Delete
            </UButton>
          </UTooltip>
        </template>
      </PageHeader>

      <UAlert
        v-if="fip.status === 'releasing'"
        color="warning"
        variant="subtle"
        icon="i-lucide-hourglass"
        :description="RELEASING"
      />
      <Panel v-if="action" as="div">
        <ActionProgress :action="action" />
      </Panel>
      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <Panel title="Details">
        <dl class="grid grid-cols-[minmax(4.5rem,8rem)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
          <dt class="text-muted">Address</dt>
          <dd class="nb-mono wrap-anywhere">{{ fip.ip }}</dd>
          <dt class="text-muted">Type</dt>
          <dd>{{ fip.type === 'ipv4' ? 'IPv4' : 'IPv6' }}</dd>
          <dt class="text-muted">Location</dt>
          <dd class="wrap-anywhere">{{ fip.location.name }}</dd>
          <dt class="text-muted">Server</dt>
          <dd class="wrap-anywhere">
            <RouterLink
              v-if="fip.server !== null"
              :to="{ name: 'server', params: { projectId, serverId: fip.server } }"
              class="text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
            >
              {{ serverName ?? 'View server' }}
            </RouterLink>
            <span v-else class="text-muted">Not assigned</span>
          </dd>
          <template v-if="fip.description">
            <dt class="text-muted">Description</dt>
            <dd class="wrap-anywhere">{{ fip.description }}</dd>
          </template>
          <dt class="text-muted">Created</dt>
          <dd>{{ formatDate(fip.created) }}</dd>
        </dl>
      </Panel>

      <AssignFloatingIPModal
        v-model:open="assigning"
        :floating-ip="fip"
        :action="assignAction"
        @started="started"
      />
      <GuestConfigDialog
        v-if="fip.guest_config"
        v-model:open="showingCommands"
        :config="fip.guest_config"
      />
      <ConfirmDialog
        v-model:open="unassigning"
        :title="`Unassign ${fip.name}?`"
        :body="`Traffic for ${fip.ip} stops reaching ${serverName ?? 'the server'}. Afterwards, run the commands shown so it stops using the address.`"
        confirm-label="Unassign"
        :loading="pending"
        @confirm="unassign"
      />
      <ConfirmByName
        v-model:open="deleting"
        :name="fip.name"
        :title="`Delete ${fip.name}?`"
        confirm-label="Delete floating IP"
        :loading="pending"
        @confirm="remove"
      />
    </template>
  </div>
</template>
