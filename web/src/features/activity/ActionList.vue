<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink } from 'vue-router'
import type { Action } from '@/api/types'
import { actionTitle } from '@/components/actionLabel'
import { useCurrentProject } from '@/lib/project'
import { useServerNames } from '@/features/servers/queries'

const props = defineProps<{ actions: Action[]; showResource: boolean }>()

const { projectId } = useCurrentProject()
const { data: index } = useServerNames(computed(() => props.showResource))

function status(a: Action): string {
  if (a.status === 'running') return `Running ${Math.round(a.progress)}%`
  return a.status === 'success' ? 'Done' : 'Failed'
}

function dot(a: Action): string {
  if (a.status === 'error') return 'bg-error'
  return a.status === 'running' ? 'bg-warning motion-safe:animate-pulse' : 'bg-success'
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

function serverOf(a: Action): { id: number; label: string; found: boolean } | null {
  const r = a.resources.find((x) => x.type === 'server')
  if (!r) return null
  const name = index.value?.names.get(r.id)
  if (name !== undefined) return { id: r.id, label: name, found: true }
  return {
    id: r.id,
    label: index.value?.complete ? 'Deleted server' : `Server ${r.id}`,
    found: false,
  }
}
const rows = computed(() => props.actions.map((a) => ({ a, srv: serverOf(a) })))
</script>

<template>
  <ul class="divide-y divide-default rounded-lg border border-default">
    <li v-for="{ a, srv } in rows" :key="a.id" class="flex flex-col gap-1 px-4 py-3">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span class="font-medium text-highlighted">{{ actionTitle(a.command) }}</span>
        <template v-if="showResource && srv">
          <RouterLink
            v-if="srv.found"
            :to="{ name: 'server', params: { projectId, serverId: srv.id } }"
            class="text-sm text-primary hover:underline"
            >{{ srv.label }}</RouterLink
          >
          <span v-else class="text-sm text-muted">{{ srv.label }}</span>
        </template>
        <span class="inline-flex items-center gap-1.5 text-sm font-medium text-default">
          <span
            data-status-dot
            aria-hidden="true"
            class="size-2 shrink-0 rounded-full"
            :class="dot(a)"
          />
          {{ status(a) }}
        </span>
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
