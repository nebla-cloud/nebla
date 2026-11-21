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
import UInput from '@nuxt/ui/components/Input.vue'
import USwitch from '@nuxt/ui/components/Switch.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import ListGroup from '@/components/ListGroup.vue'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import TerminalBlock from '@/components/TerminalBlock.vue'
import { useCopy } from '@/lib/useCopy'
import { useAdminLocations } from '@/lib/adminQueries'
import LocationForm from './LocationForm.vue'
import {
  useDeleteLocation,
  useEnrollments,
  useStartEnrollment,
  useSyncNodes,
  useUpdateNode,
} from './queries'

const route = useRoute()
const router = useRouter()
const toast = useToast()
const id = computed(() => Number(route.params.locationId))
const { data: locations, isPending, error: loadError } = useAdminLocations()
const location = computed(() => locations.value?.find((l) => l.id === id.value))

const sync = useSyncNodes()
const updateNode = useUpdateNode(id.value)
const del = useDeleteLocation()
const rerun = useStartEnrollment()
const { data: enrollments } = useEnrollments()
// A live Re-run is resumed rather than replaced, so a reported one isn't lost.
const liveRerun = computed(() =>
  enrollments.value?.find(
    (e) => e.location_id === id.value && !e.expired && Date.parse(e.expires_at) > Date.now(),
  ),
)
// Every storage volumes may use here; granting one that already has the role is harmless.
const volumeGrants = computed(() => {
  const l = location.value
  if (!l || l.volume_storage_ok || !l.nodes.length) return []
  const ids = l.volume_storage
    ? [l.volume_storage]
    : l.nodes.filter((n) => n.enabled).map((n) => n.disk_storage_override || l.disk_storage)
  return [...new Set(ids)].map(
    (s) => `pveum acl modify /storage/${s} --users nebla@pve --roles NeblaISO`,
  )
})
const grantCopy = useCopy()
const cleanup = ref<string[]>(readCleanup())
const editing = ref(false)
const confirmOpen = ref(false)
const actionError = ref<string | null>(null)
const override = ref<Record<number, string>>({})

function readCleanup(): string[] {
  const c = (router.options.history.state as { cleanup?: unknown }).cleanup
  if (!Array.isArray(c)) return []
  // router.replace merges state, so null it in place to keep it from showing again.
  window.history.replaceState({ ...window.history.state, cleanup: null }, '')
  return c.map(String)
}

async function copyCleanup() {
  try {
    await navigator.clipboard.writeText(cleanup.value.join('\n'))
    toast.add({ title: 'Copied', color: 'success' })
  } catch {
    toast.add({ title: "Couldn't copy", color: 'error' })
  }
}

async function run(fn: () => Promise<unknown>, done?: string) {
  actionError.value = null
  try {
    await fn()
    if (done) toast.add({ title: done, color: 'success' })
  } catch (e) {
    actionError.value = errorMessage(e)
  }
}

const syncNodes = () => run(() => sync.mutateAsync(id.value), 'Nodes synced')
const toggle = (nodeId: number, enabled: boolean) =>
  run(() => updateNode.mutateAsync({ nodeId, body: { enabled } }))
const saveOverride = (nodeId: number, current: string) =>
  run(
    () =>
      updateNode.mutateAsync({
        nodeId,
        body: { disk_storage_override: (override.value[nodeId] ?? current).trim() },
      }),
    'Storage override saved',
  )

const startRerun = () =>
  run(async () => {
    const c = await rerun.mutateAsync({ location_id: id.value })
    await router.push({ name: 'admin-location-enroll', params: { enrollmentId: c.enrollment.id } })
  })

async function remove() {
  actionError.value = null
  try {
    await del.mutateAsync(id.value)
    confirmOpen.value = false
    await router.push({ name: 'admin-locations' })
  } catch (e) {
    confirmOpen.value = false
    actionError.value = errorMessage(e)
  }
}

const gb = (bytes: number) => `${Math.round(bytes / 1024 ** 3)} GB`
</script>

<template>
  <div class="flex flex-col gap-4">
    <RouterLink :to="{ name: 'admin-locations' }" class="text-sm text-muted hover:text-highlighted">
      Back to locations
    </RouterLink>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading location</p>
    <UAlert
      v-else-if="!location"
      color="neutral"
      variant="subtle"
      description="This location doesn't exist."
    />

    <template v-else>
      <PageHeader :title="location.name" :description="location.description || undefined">
        <template #meta>
          <span class="nb-mono truncate text-sm font-normal text-muted">{{
            location.pve.url
          }}</span>
        </template>
        <template #actions>
          <UButton v-if="!editing" color="neutral" variant="outline" @click="editing = true">
            Edit connection
          </UButton>
          <UButton
            v-if="liveRerun"
            color="neutral"
            variant="outline"
            :to="{ name: 'admin-location-enroll', params: { enrollmentId: liveRerun.id } }"
          >
            Re-run setup
          </UButton>
          <UButton
            v-else
            color="neutral"
            variant="outline"
            :loading="rerun.isPending.value"
            @click="startRerun"
          >
            Re-run setup
          </UButton>
          <UButton color="error" variant="outline" @click="confirmOpen = true">
            Delete location
          </UButton>
        </template>
      </PageHeader>

      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <UAlert
        v-if="location.firewall_enabled === false"
        color="warning"
        variant="subtle"
        description="The Proxmox datacenter firewall is off at this location. Server firewalls and anti-spoofing filters aren't enforced."
      />

      <UAlert
        v-if="volumeGrants.length"
        data-testid="volume-grant"
        color="warning"
        variant="subtle"
        title="Volumes need one more permission"
      >
        <template #description>
          <p>Re-run the setup command, or run:</p>
          <TerminalBlock as="pre" class="mt-2">{{ volumeGrants.join('\n') }}</TerminalBlock>
          <UButton
            class="mt-2"
            color="neutral"
            variant="outline"
            size="xs"
            @click="grantCopy.copy(volumeGrants.join('\n'))"
          >
            {{ grantCopy.copied.value ? 'Copied' : 'Copy' }}
          </UButton>
        </template>
      </UAlert>

      <UAlert
        v-if="cleanup.length"
        color="warning"
        variant="subtle"
        title="Optional cleanup on the node"
        description="Run these on the Proxmox node to remove the old setup."
        close
        @update:open="cleanup = []"
      >
        <template #description>
          <p>Run these on the Proxmox node to remove the old setup.</p>
          <TerminalBlock as="pre" data-testid="enroll-cleanup" class="mt-2">{{
            cleanup.join('\n')
          }}</TerminalBlock>
          <UButton class="mt-2" color="neutral" variant="outline" size="xs" @click="copyCleanup">
            Copy
          </UButton>
        </template>
      </UAlert>

      <Panel v-if="editing" title="Edit connection">
        <LocationForm
          :location="location"
          @cancel="editing = false"
          @saved="((editing = false), toast.add({ title: 'Location saved', color: 'success' }))"
        />
      </Panel>

      <section class="flex flex-col gap-3">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div class="flex max-w-prose flex-col gap-1">
            <h2 class="nb-h2 text-highlighted">Nodes</h2>
            <p class="text-sm text-muted">
              A node is one Proxmox host in the cluster. Servers are only placed on enabled nodes.
              Syncing finds new nodes and disables ones that have vanished.
            </p>
          </div>
          <UButton
            color="neutral"
            variant="outline"
            icon="i-lucide-refresh-cw"
            :loading="sync.isPending.value"
            @click="syncNodes"
          >
            Sync nodes
          </UButton>
        </div>

        <p v-if="!location.nodes.length" class="text-muted">
          No nodes found yet. Sync to ask Proxmox again.
        </p>
        <ListGroup v-else>
          <li v-for="n in location.nodes" :key="n.id" class="flex flex-col gap-3 p-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <div class="flex flex-col">
                <span class="font-bold text-highlighted">{{ n.name }}</span>
                <span class="text-sm text-muted">
                  {{ n.status }}, {{ n.max_cpu }} cores, {{ gb(n.mem) }} of
                  {{ gb(n.max_mem) }} memory used
                </span>
              </div>
              <USwitch
                :model-value="n.enabled"
                :label="n.enabled ? 'Enabled' : 'Disabled'"
                :aria-label="`${n.enabled ? 'Disable' : 'Enable'} ${n.name}`"
                @update:model-value="(v: boolean) => toggle(n.id, v)"
              />
            </div>
            <form
              class="flex flex-col gap-1 sm:max-w-md"
              @submit.prevent="saveOverride(n.id, n.disk_storage_override)"
            >
              <label class="text-sm font-medium text-highlighted" :for="`override-${n.id}`">
                Disk storage override
              </label>
              <div class="flex gap-2">
                <UInput
                  :id="`override-${n.id}`"
                  class="flex-1"
                  :placeholder="location.disk_storage"
                  :model-value="override[n.id] ?? n.disk_storage_override"
                  @update:model-value="(v) => (override[n.id] = String(v))"
                />
                <UButton type="submit" color="neutral" variant="outline">Save</UButton>
              </div>
              <p class="text-xs text-muted">
                Use a different storage on this node. Leave blank to use
                {{ location.disk_storage }}.
              </p>
            </form>
          </li>
        </ListGroup>
      </section>

      <ConfirmByName
        v-model:open="confirmOpen"
        :name="location.name"
        title="Delete this location"
        confirm-label="Delete"
        :loading="del.isPending.value"
        @confirm="remove"
      />
    </template>
  </div>
</template>
