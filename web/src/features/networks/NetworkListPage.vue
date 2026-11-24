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
import type { Network } from '@/api/types'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import { useCurrentProject } from '@/lib/project'
import CreateNetworkModal from './CreateNetworkModal.vue'
import { useNetworks, useRefreshNetworks } from './queries'

const toast = useToast()
const router = useRouter()
const { projectId } = useCurrentProject()
const { data: networks, isPending, error } = useNetworks()
const refresh = useRefreshNetworks()
const creating = ref(false)
const hasNetworks = computed(() => !!networks.value?.length)

const count = (n: Network) => `${n.servers.length} ${n.servers.length === 1 ? 'server' : 'servers'}`

async function created(n: Network) {
  toast.add({ title: `Created ${n.name}`, color: 'success' })
  void refresh()
  await router.push({ name: 'network', params: { projectId: projectId.value, networkId: n.id } })
}
</script>

<template>
  <div class="flex flex-col gap-5">
    <PageHeader
      title="Networks"
      description="Private networks that connect your servers in one location."
    >
      <template v-if="hasNetworks" #actions>
        <UButton icon="i-lucide-plus" @click="creating = true">Create network</UButton>
      </template>
    </PageHeader>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p v-else-if="isPending" class="text-muted">Loading networks…</p>
    <ListGroup v-else-if="hasNetworks" label="Networks">
      <ListRow
        v-for="n in networks"
        :key="n.id"
        :to="{ name: 'network', params: { projectId, networkId: n.id } }"
      >
        <span class="flex min-w-0 flex-wrap items-center justify-between gap-x-4 gap-y-1">
          <span class="min-w-0 truncate">{{ n.name }}</span>
          <span
            class="flex min-w-0 flex-wrap gap-x-4 text-sm font-normal whitespace-normal text-muted"
          >
            <span class="nb-mono wrap-anywhere">{{ n.ip_range }}</span>
            <span class="hidden sm:inline">{{ n.location.name }}</span>
            <span>{{ count(n) }}</span>
          </span>
        </span>
      </ListRow>
    </ListGroup>
    <EmptyState
      v-else
      title="No networks yet"
      body="A private network lets servers in the same location talk to each other without using the internet."
    >
      <UButton icon="i-lucide-plus" @click="creating = true">Create network</UButton>
    </EmptyState>

    <CreateNetworkModal v-model:open="creating" @created="created" />
  </div>
</template>
