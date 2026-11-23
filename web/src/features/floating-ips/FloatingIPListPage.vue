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
import { useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { errorMessage } from '@/api/errors'
import type { FloatingIp } from '@/api/types'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useServerNames } from '@/features/servers/queries'
import { useCurrentProject } from '@/lib/project'
import CreateFloatingIPModal from './CreateFloatingIPModal.vue'
import { useFloatingIPs, useRefreshFloatingIPs } from './queries'

const toast = useToast()
const router = useRouter()
const { projectId } = useCurrentProject()
const { data: floatingIps, isPending, error } = useFloatingIPs()
const anyAssigned = computed(() => !!floatingIps.value?.some((f) => f.server !== null))
const { data: names } = useServerNames(anyAssigned)
const refresh = useRefreshFloatingIPs()
const creating = ref(false)

function serverName(f: FloatingIp): string {
  if (f.server === null) return 'Not assigned'
  return names.value?.names.get(f.server) ?? 'Assigned'
}

async function created(f: FloatingIp) {
  toast.add({ title: `Created ${f.name}`, color: 'success' })
  void refresh()
  await router.push({
    name: 'floating-ip',
    params: { projectId: projectId.value, floatingIpId: f.id },
  })
}
</script>

<template>
  <div class="flex flex-col gap-5">
    <PageHeader
      title="Floating IPs"
      description="Addresses you can move between servers in the same location."
    >
      <template v-if="floatingIps?.length" #actions>
        <UButton icon="i-lucide-plus" @click="creating = true">Create floating IP</UButton>
      </template>
    </PageHeader>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p v-else-if="isPending" class="text-muted">Loading floating IPs…</p>
    <ListGroup v-else-if="floatingIps?.length" label="Floating IPs">
      <ListRow
        v-for="f in floatingIps"
        :key="f.id"
        :to="{ name: 'floating-ip', params: { projectId, floatingIpId: f.id } }"
      >
        <template #leading><StatusDot kind="floating-ip" :status="f.status" /></template>
        <span class="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <span class="min-w-0 truncate">{{ f.name }}</span>
          <span
            class="flex min-w-0 flex-wrap gap-x-4 text-sm font-normal whitespace-normal text-muted"
          >
            <span class="nb-mono wrap-anywhere">{{ f.ip }}</span>
            <span class="hidden sm:inline">{{ f.location.name }}</span>
            <span class="wrap-anywhere">{{ serverName(f) }}</span>
          </span>
        </span>
      </ListRow>
    </ListGroup>
    <EmptyState
      v-else
      title="No floating IPs yet"
      body="A floating IP is an address you can move from one server to another in the same location, so traffic follows it without changing DNS."
    >
      <UButton icon="i-lucide-plus" @click="creating = true">Create floating IP</UButton>
    </EmptyState>

    <CreateFloatingIPModal v-model:open="creating" @created="created" />
  </div>
</template>
