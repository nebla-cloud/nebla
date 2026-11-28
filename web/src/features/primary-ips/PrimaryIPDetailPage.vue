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
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UTooltip from '@nuxt/ui/components/Tooltip.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
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
import { useServer } from '@/features/servers/queries'
import { useCrumbs } from '@/lib/crumbs'
import { formatDate } from '@/lib/format'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import { primaryIpStatus } from '@/lib/status'
import { useActionPoll } from '@/lib/useActionPoll'
import AssignPrimaryIPModal from './AssignPrimaryIPModal.vue'
import {
  deletePrimaryIP,
  noPublic,
  OLD_SERVER,
  primaryIPDone,
  STOP_FIRST,
  TAKES_EFFECT,
  unassignPrimaryIP,
  updatePrimaryIP,
  usePrimaryIP,
  useRefreshPrimaryIPs,
} from './queries'

const RELEASING = 'The address is still being released from the previous server.'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const refresh = useRefreshPrimaryIPs()
const { projectId } = useCurrentProject()
const primaryIpId = computed(() => parseProjectId(route.params.primaryIpId) ?? 0)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: pip, error: loadError } = usePrimaryIP(primaryIpId, running)
const assignedId = computed(() => pip.value?.server?.id ?? 0)
const { data: assignedServer } = useServer(assignedId)
const assigned = computed(() => pip.value?.server != null)

useCrumbs(() => [
  { label: 'Primary IPs', to: { name: 'primary-ips', params: { projectId: projectId.value } } },
  { label: pip.value?.name ?? 'Primary IP' },
])

const protectionFlags = [
  {
    key: 'delete' as const,
    label: 'Delete',
    description: "Blocks deleting this primary IP until it's turned off.",
  },
]

const assigning = ref(false)
const unassigning = ref(false)
const deleting = ref(false)
const pending = ref(false)
const warnings = ref<string[]>([])

const busy = computed(
  () => running.value || pending.value || (!!pip.value && pip.value.status !== 'active'),
)
// The server's state is unknown until it loads, so the swap stays off until it says "off".
const serverOff = computed(() => assignedServer.value?.status === 'off')
const unassignBlocked = computed(() => busy.value || !serverOff.value)
const status = computed(() => (pip.value ? primaryIpStatus(pip.value.status) : null))
const assignAction = computed(() =>
  action.value?.command === 'assign_primary_ip' ? action.value : undefined,
)
const notFound = computed(
  () => !pip.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)

function started(a: Action, w: string[] = []) {
  lastName.value = pip.value?.name ?? ''
  warnings.value = w
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void refresh()
}

async function finished(a: Action) {
  const name = lastName.value || pip.value?.name || ''
  await refresh()
  const verb = primaryIPDone[a.command]
  if (verb) toast.add({ title: `${name} ${verb}`, color: 'success' })
  assigning.value = false
}

async function unassign() {
  const id = primaryIpId.value
  pending.value = true
  try {
    const res = await unassignPrimaryIP(id)
    started(res.action, res.warnings)
    unassigning.value = false
  } catch (e) {
    toast.add({
      title: `Couldn't unassign ${pip.value?.name ?? 'the primary IP'}`,
      description: errorMessage(e),
      color: 'error',
    })
  } finally {
    pending.value = false
  }
}

async function remove() {
  const name = pip.value?.name ?? ''
  pending.value = true
  try {
    await deletePrimaryIP(primaryIpId.value)
    await refresh()
    toast.add({ title: `${name} deleted`, color: 'success' })
    await router.push({ name: 'primary-ips', params: { projectId: projectId.value } })
  } catch (e) {
    deleting.value = false
    toast.add({ title: `Couldn't delete ${name}`, description: errorMessage(e), color: 'error' })
  } finally {
    pending.value = false
  }
}

async function setAutoDelete(v: boolean) {
  pending.value = true
  try {
    await updatePrimaryIP(primaryIpId.value, { auto_delete: v })
    await refresh()
  } catch (e) {
    toast.add({ title: "Couldn't save the change", description: errorMessage(e), color: 'error' })
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
      description="This primary IP doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !pip"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!pip" class="text-muted">Loading primary IP…</p>

    <template v-else>
      <PageHeader :title="pip.name" class="max-sm:flex-col max-sm:items-stretch">
        <template #meta>
          <span class="inline-flex items-center gap-1.5 font-medium">
            <StatusDot kind="primary-ip" :status="pip.status" />{{ status?.label }}
          </span>
        </template>
        <template #actions>
          <UButton
            v-if="!assigned"
            color="neutral"
            variant="outline"
            :disabled="busy"
            @click="assigning = true"
          >
            Assign
          </UButton>
          <UButton
            v-else
            color="neutral"
            variant="outline"
            :disabled="unassignBlocked"
            @click="unassigning = true"
          >
            Unassign
          </UButton>
          <ProtectionButton
            :flags="protectionFlags"
            :value="pip.protection"
            :save="(c) => updatePrimaryIP(primaryIpId, { protection: { delete: c.delete } })"
            @saved="refresh"
          />
          <UTooltip
            :text="pip.protection.delete ? 'Turn off delete protection first.' : undefined"
            :disabled="!pip.protection.delete"
          >
            <UButton
              color="error"
              variant="outline"
              :icon="pip.protection.delete ? 'i-lucide-lock' : undefined"
              :disabled="busy || assigned || pip.protection.delete"
              @click="deleting = true"
            >
              Delete
            </UButton>
          </UTooltip>
        </template>
      </PageHeader>

      <p v-if="assigned && assignedServer && !serverOff" class="text-sm text-muted">
        {{ STOP_FIRST }}
      </p>
      <p v-else-if="assigned" class="text-sm text-muted">Unassign it first to delete it.</p>
      <UAlert
        v-if="pip.status === 'releasing'"
        color="warning"
        variant="subtle"
        icon="i-lucide-hourglass"
        :description="RELEASING"
      />
      <UAlert
        v-for="w in warnings"
        :key="w"
        color="warning"
        variant="subtle"
        icon="i-lucide-triangle-alert"
        :description="w"
      />
      <Panel v-if="action" as="div">
        <ActionProgress :action="action" />
        <p v-if="action.status === 'success'" class="mt-2 text-sm text-muted">{{ TAKES_EFFECT }}</p>
      </Panel>
      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <Panel title="Details">
        <dl class="grid grid-cols-[minmax(4.5rem,8rem)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
          <dt class="text-muted">Address</dt>
          <dd class="nb-mono wrap-anywhere">{{ pip.ip }}</dd>
          <dt class="text-muted">Type</dt>
          <dd>{{ pip.type === 'ipv4' ? 'IPv4' : 'IPv6' }}</dd>
          <dt class="text-muted">Location</dt>
          <dd class="wrap-anywhere">{{ pip.location.name }}</dd>
          <dt class="text-muted">Server</dt>
          <dd class="wrap-anywhere">
            <RouterLink
              v-if="pip.server !== null"
              :to="{ name: 'server', params: { projectId, serverId: pip.server.id } }"
              class="text-highlighted underline underline-offset-2 hover:text-primary focus-visible:outline-2 focus-visible:outline-primary"
            >
              {{ pip.server.name }}
            </RouterLink>
            <span v-else class="text-muted">Not assigned</span>
          </dd>
          <template v-if="pip.description">
            <dt class="text-muted">Description</dt>
            <dd class="wrap-anywhere">{{ pip.description }}</dd>
          </template>
          <dt class="text-muted">Created</dt>
          <dd>{{ formatDate(pip.created) }}</dd>
        </dl>
      </Panel>

      <Panel title="Server deletion">
        <USwitch
          :model-value="pip.auto_delete"
          :disabled="pending"
          label="Delete with its server"
          description="Off keeps the address in this project when its server is deleted."
          @update:model-value="setAutoDelete"
        />
      </Panel>

      <AssignPrimaryIPModal
        v-model:open="assigning"
        :primary-ip="pip"
        :action="assignAction"
        @started="started"
      />
      <ConfirmDialog
        v-model:open="unassigning"
        :title="`Unassign ${pip.name}?`"
        :body="`${noPublic(pip.type)} ${TAKES_EFFECT}${assignedServer?.ip_change_reruns_cloud_init ? ` ${OLD_SERVER}` : ''}`"
        confirm-label="Unassign"
        :loading="pending"
        @confirm="unassign"
      />
      <ConfirmByName
        v-model:open="deleting"
        :name="pip.name"
        :title="`Delete ${pip.name}?`"
        confirm-label="Delete primary IP"
        :loading="pending"
        @confirm="remove"
      />
    </template>
  </div>
</template>
