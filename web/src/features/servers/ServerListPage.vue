<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useCurrentProject } from '@/lib/project'
import { serverStatus } from '@/lib/status'
import { useServers } from './queries'

const { projectId } = useCurrentProject()
const newRoute = computed(() => ({ name: 'server-new', params: { projectId: projectId.value } }))
const { data: servers, isPending, error } = useServers()
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between gap-3">
      <h1 class="text-xl font-bold tracking-tight text-highlighted">Servers</h1>
      <UButton v-if="servers?.length" :to="newRoute">Create server</UButton>
    </div>

    <UAlert v-if="error" color="error" variant="subtle" :description="errorMessage(error)" />
    <p v-else-if="isPending" class="text-muted">Loading servers…</p>
    <EmptyState
      v-else-if="!servers?.length"
      title="No servers yet"
      body="Create a server to get started. It is ready in about a minute."
    >
      <UButton :to="newRoute">Create server</UButton>
    </EmptyState>
    <ul v-else class="flex flex-col divide-y divide-default rounded-lg border border-default">
      <li v-for="s in servers" :key="s.id">
        <RouterLink
          :to="{ name: 'server', params: { projectId, serverId: s.id } }"
          class="grid grid-cols-[auto_1fr] items-center gap-x-3 gap-y-1 px-4 py-3 hover:bg-elevated/60 focus-visible:outline-2 focus-visible:outline-primary sm:grid-cols-[auto_minmax(0,1.2fr)_repeat(4,minmax(0,1fr))]"
        >
          <StatusDot :status="s.status" />
          <span class="truncate font-semibold text-highlighted">{{ s.name }}</span>
          <span
            class="col-start-2 flex flex-wrap gap-x-4 text-sm text-muted sm:contents [&>span]:truncate"
          >
            <span>{{ s.image.name }}</span>
            <span>{{ s.server_type.name }}</span>
            <span>{{
              s.status === 'running' ? (s.public_net.ipv4?.ip ?? '—') : serverStatus(s.status).label
            }}</span>
            <span>{{ s.location.name }}</span>
          </span>
        </RouterLink>
      </li>
    </ul>
  </div>
</template>
