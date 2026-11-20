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
import { useAdminLocations } from '@/lib/adminQueries'
import LocationForm from './LocationForm.vue'
import { useDeleteLocation, useStartEnrollment, useSyncNodes, useUpdateNode } from './queries'

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
const cleanup = ref<string[]>(readCleanup())
const editing = ref(false)
const confirmOpen = ref(false)
const actionError = ref<string | null>(null)
const override = ref<Record<number, string>>({})

function readCleanup(): string[] {
  const c = (router.options.history.state as { cleanup?: unknown }).cleanup
  if (!Array.isArray(c)) return []
  // Clear it so a later visit doesn't show the alert again.
  void router.replace({ name: 'admin-location', params: route.params, state: {} })
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
  <div class="flex flex-col gap-6 p-4 sm:p-8">
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
      <header class="flex flex-wrap items-start justify-between gap-3">
        <div class="flex min-w-0 flex-col gap-1">
          <h1 class="text-2xl font-bold tracking-tight text-highlighted">{{ location.name }}</h1>
          <p v-if="location.description" class="text-muted">{{ location.description }}</p>
          <p class="truncate text-sm text-muted">{{ location.pve.url }}</p>
        </div>
        <div class="flex gap-2">
          <UButton v-if="!editing" color="neutral" variant="outline" @click="editing = true">
            Edit connection
          </UButton>
          <UButton
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
        </div>
      </header>

      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

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
          <pre
            data-testid="enroll-cleanup"
            class="mt-2 overflow-x-auto rounded border border-default bg-default p-2 text-xs whitespace-pre-wrap break-all"
            >{{ cleanup.join('\n') }}</pre>
          <UButton class="mt-2" color="neutral" variant="outline" size="xs" @click="copyCleanup">
            Copy
          </UButton>
        </template>
      </UAlert>

      <section v-if="editing" class="rounded-lg border border-default p-4 sm:p-6">
        <h2 class="mb-4 text-lg font-bold tracking-tight text-highlighted">Edit connection</h2>
        <LocationForm
          :location="location"
          @cancel="editing = false"
          @saved="((editing = false), toast.add({ title: 'Location saved', color: 'success' }))"
        />
      </section>

      <section class="flex flex-col gap-3">
        <div class="flex flex-wrap items-end justify-between gap-3">
          <div class="flex max-w-prose flex-col gap-1">
            <h2 class="text-lg font-bold tracking-tight text-highlighted">Nodes</h2>
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
        <ul v-else class="flex flex-col divide-y divide-default rounded-lg border border-default">
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
        </ul>
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
