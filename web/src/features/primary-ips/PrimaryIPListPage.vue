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
import type { PrimaryIp } from '@/api/types'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import ProtectedLock from '@/components/ProtectedLock.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useCurrentProject } from '@/lib/project'
import CreatePrimaryIPModal from './CreatePrimaryIPModal.vue'
import { usePrimaryIPs, useRefreshPrimaryIPs } from './queries'

const toast = useToast()
const router = useRouter()
const { projectId } = useCurrentProject()
const { data: primaryIps, isPending, error } = usePrimaryIPs()
const refresh = useRefreshPrimaryIPs()
const creating = ref(false)
const rows = computed(() => primaryIps.value ?? [])

async function created(p: PrimaryIp) {
  toast.add({ title: `Created ${p.name}`, color: 'success' })
  void refresh()
  await router.push({
    name: 'primary-ip',
    params: { projectId: projectId.value, primaryIpId: p.id },
  })
}
</script>

<template>
  <div class="flex flex-col gap-5">
    <PageHeader
      title="Primary IPs"
      description="The public addresses of your servers. Keep one when a server goes, and use it again later."
    >
      <template v-if="rows.length" #actions>
        <UButton icon="i-lucide-plus" @click="creating = true">Create primary IP</UButton>
      </template>
    </PageHeader>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p v-else-if="isPending" class="text-muted">Loading primary IPs…</p>
    <ListGroup v-else-if="rows.length" label="Primary IPs">
      <ListRow
        v-for="p in rows"
        :key="p.id"
        :to="{ name: 'primary-ip', params: { projectId, primaryIpId: p.id } }"
      >
        <template #leading><StatusDot kind="primary-ip" :status="p.status" /></template>
        <span class="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <span class="flex min-w-0 items-center gap-1.5">
            <ProtectedLock v-if="p.protection.delete" />
            <span class="min-w-0 truncate">{{ p.name }}</span>
          </span>
          <span
            class="flex min-w-0 flex-wrap gap-x-4 text-sm font-normal whitespace-normal text-muted"
          >
            <span class="nb-mono wrap-anywhere">{{ p.ip }}</span>
            <span>{{ p.type === 'ipv4' ? 'IPv4' : 'IPv6' }}</span>
            <span class="hidden sm:inline">{{ p.location.name }}</span>
            <span class="wrap-anywhere">{{ p.server?.name ?? 'Not assigned' }}</span>
            <span>{{ p.auto_delete ? 'Deleted with server' : 'Kept' }}</span>
          </span>
        </span>
      </ListRow>
    </ListGroup>
    <EmptyState
      v-else
      title="No primary IPs yet"
      body="Each server gets its public addresses as primary IPs. Reserve one here to keep an address, or to use it when you create a server."
    >
      <UButton icon="i-lucide-plus" @click="creating = true">Create primary IP</UButton>
    </EmptyState>

    <CreatePrimaryIPModal v-model:open="creating" @created="created" />
  </div>
</template>
