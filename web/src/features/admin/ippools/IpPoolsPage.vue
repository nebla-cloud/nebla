<script setup lang="ts">
import { computed, ref } from 'vue'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UModal from '@nuxt/ui/components/Modal.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import ConfirmByName from '@/components/ConfirmByName.vue'
import EmptyState from '@/components/EmptyState.vue'
import { useAdminLocations } from '@/lib/adminQueries'
import { rangeSize } from './cidr'
import IpPoolForm from './IpPoolForm.vue'
import { useAdminIpPools, useDeleteIpPool, type IpPool } from './queries'

const toast = useToast()
const { data: pools, isPending, error: loadError } = useAdminIpPools()
const { data: locations } = useAdminLocations()
const del = useDeleteIpPool()

const formOpen = ref(false)
const editing = ref<IpPool | undefined>()
const removing = ref<IpPool | null>(null)
const removeOpen = computed({
  get: () => removing.value !== null,
  set: (v) => {
    if (!v) removing.value = null
  },
})

function openForm(p?: IpPool) {
  editing.value = p
  formOpen.value = true
}

function locationName(id: number): string {
  return locations.value?.find((l) => l.id === id)?.name ?? `Location ${id}`
}

function usage(p: IpPool): string {
  return `${p.allocated} of ${rangeSize(p.range_start, p.range_end)} addresses in use`
}

function saved() {
  formOpen.value = false
  toast.add({ title: 'IP pool saved', color: 'success' })
}

async function remove() {
  if (!removing.value) return
  try {
    await del.mutateAsync(removing.value.id)
    toast.add({ title: `Deleted ${removing.value.cidr}`, color: 'success' })
    removing.value = null
  } catch (e) {
    toast.add({ title: errorMessage(e), color: 'error' })
  }
}
</script>

<template>
  <div class="flex flex-col gap-6">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h1 class="text-2xl font-extrabold tracking-tight text-highlighted">IP pools</h1>
      <UButton icon="i-lucide-plus" @click="openForm()">New IP pool</UButton>
    </div>

    <UAlert
      v-if="loadError"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading IP pools…</p>
    <EmptyState
      v-else-if="!pools?.length"
      title="No IP pools yet"
      body="Servers get their public addresses from a pool. Add one for each network a location offers."
    >
      <UButton @click="openForm()">New IP pool</UButton>
    </EmptyState>
    <ul v-else class="divide-y divide-default overflow-hidden rounded-lg border border-default">
      <li
        v-for="p in pools"
        :key="p.id"
        class="flex flex-wrap items-center justify-between gap-3 px-4 py-3.5"
      >
        <div class="flex min-w-0 flex-col gap-0.5">
          <span class="truncate font-semibold text-highlighted">{{ p.cidr }}</span>
          <span class="text-sm text-muted">
            {{ locationName(p.location_id) }}, {{ p.family === 'ipv4' ? 'IPv4' : 'IPv6' }}, bridge
            {{ p.bridge }}<template v-if="p.vlan_tag">, VLAN {{ p.vlan_tag }}</template>
          </span>
          <span class="text-sm text-muted">
            {{ p.range_start }} to {{ p.range_end }}, gateway {{ p.gateway }}
          </span>
          <span class="text-sm text-default">{{ usage(p) }}</span>
        </div>
        <div class="flex gap-1">
          <UButton
            color="neutral"
            variant="ghost"
            :aria-label="`Edit ${p.cidr}`"
            @click="openForm(p)"
          >
            Edit
          </UButton>
          <UButton
            color="error"
            variant="ghost"
            :aria-label="`Delete ${p.cidr}`"
            @click="removing = p"
          >
            Delete
          </UButton>
        </div>
      </li>
    </ul>

    <UModal
      v-model:open="formOpen"
      :title="editing ? `Edit ${editing.cidr}` : 'New IP pool'"
      description="Addresses in the range are handed out to new servers."
    >
      <template #body>
        <IpPoolForm
          :key="editing?.id ?? 'new'"
          :item="editing"
          @done="saved"
          @cancel="formOpen = false"
        />
      </template>
    </UModal>

    <ConfirmByName
      v-model:open="removeOpen"
      :name="removing?.cidr ?? ''"
      title="Delete this IP pool?"
      confirm-label="Delete IP pool"
      :loading="del.isPending.value"
      @confirm="remove"
    />
  </div>
</template>
