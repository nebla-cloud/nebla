<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { Action } from '@/api/types'
import { actionTitle } from '@/components/actionLabel'
import { useServers } from '@/features/servers/queries'

const props = defineProps<{ actions: Action[]; showResource: boolean }>()

const { data: servers } = useServers()
const names = computed(() => new Map((servers.value ?? []).map((s) => [s.id, s.name])))

function status(a: Action): string {
  if (a.status === 'running') return `Running ${Math.round(a.progress)}%`
  return a.status === 'success' ? 'Done' : 'Failed'
}

function tone(a: Action): string {
  if (a.status === 'error') return 'text-error'
  return a.status === 'running' ? 'text-warning' : 'text-success'
}

function relative(iso: string): string {
  const s = Math.max(0, Math.round((Date.now() - new Date(iso).getTime()) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86400)} d ago`
}

function duration(a: Action): string {
  const end = a.finished ? new Date(a.finished).getTime() : Date.now()
  const s = Math.max(0, Math.round((end - new Date(a.started).getTime()) / 1000))
  return s < 60 ? `${s} s` : `${Math.round(s / 60)} min`
}

function serverOf(a: Action): { id: number; label: string } | null {
  const r = a.resources.find((x) => x.type === 'server')
  if (!r) return null
  return { id: r.id, label: names.value.get(r.id) ?? (servers.value ? 'Deleted server' : `Server ${r.id}`) }
}
const rows = computed(() => props.actions.map((a) => ({ a, srv: serverOf(a) })))
</script>

<template>
  <ul class="divide-y divide-default rounded-lg border border-default">
    <li v-for="{ a, srv } in rows" :key="a.id" class="flex flex-col gap-1 px-4 py-3">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span class="font-medium text-highlighted">{{ actionTitle(a.command) }}</span>
        <RouterLink
          v-if="showResource && srv"
          :to="{ name: 'server', params: { serverId: srv.id } }"
          class="text-sm text-primary hover:underline"
          >{{ srv.label }}</RouterLink
        >
        <span class="text-sm font-medium" :class="tone(a)">{{ status(a) }}</span>
        <span class="ms-auto flex gap-3 text-sm text-muted">
          <time :datetime="a.started" :title="new Date(a.started).toLocaleString('en-GB')">{{
            relative(a.started)
          }}</time>
          <span>{{ duration(a) }}</span>
        </span>
      </div>
      <p v-if="a.status === 'error' && a.error" class="text-sm text-error">
        {{ a.error.message }}
      </p>
    </li>
  </ul>
</template>
