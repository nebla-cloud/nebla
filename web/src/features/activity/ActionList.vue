<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import type { components } from '@/api/schema'
import type { Action } from '@/api/types'
import { actionTitle } from '@/components/actionLabel'
import TechnicalDetails from '@/components/TechnicalDetails.vue'
import { parseProjectId, useCurrentProject } from '@/lib/project'
import { useSession } from '@/lib/session'
import { useServerNames } from '@/features/servers/queries'

type AdminAction = components['schemas']['AdminAction']
type Row = Action | AdminAction

const props = defineProps<{
  actions: Row[]
  showResource: boolean
  projectNames?: boolean
  admin?: boolean
}>()

const route = useRoute()
const { me } = useSession()
const inProject = route.meta.side !== 'admin' && parseProjectId(route.params.projectId) !== null
const projectId = inProject ? useCurrentProject().projectId : ref(0)
const { data: index } = inProject
  ? useServerNames(computed(() => props.showResource))
  : { data: ref(undefined) }
const details = computed(() => props.admin || me.value?.is_admin === true)

function projectOf(a: Row): { id: number; name: string } | null {
  return 'project_name' in a ? { id: a.project_id, name: a.project_name } : null
}

function inlineDetail(a: Row): { code: string; detail: string } | undefined {
  if (!a.error || !('detail' in a.error)) return undefined
  return { code: a.error.code, detail: a.error.detail }
}

function status(a: Row): string {
  if (a.status === 'running') return `Running ${Math.round(a.progress)}%`
  return a.status === 'success' ? 'Done' : 'Failed'
}

function dot(a: Row): string {
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

function duration(a: Row): string {
  const end = a.finished ? new Date(a.finished).getTime() : Date.now()
  const s = Math.max(0, Math.round((end - new Date(a.started).getTime()) / 1000))
  return s < 60 ? `${s} s` : `${Math.round(s / 60)} min`
}

function serverOf(a: Row): { id: number; label: string; found: boolean } | null {
  const r = a.resources.find((x) => x.type === 'server')
  if (!r || !inProject) return null
  const name = index.value?.names.get(r.id)
  if (name !== undefined) return { id: r.id, label: name, found: true }
  return {
    id: r.id,
    label: index.value?.complete ? 'Deleted server' : `Server ${r.id}`,
    found: false,
  }
}
const rows = computed(() =>
  props.actions.map((a) => ({
    a,
    srv: serverOf(a),
    proj: props.projectNames ? projectOf(a) : null,
  })),
)
</script>

<template>
  <ul class="divide-y divide-default rounded-lg border border-default">
    <li v-for="{ a, srv, proj } in rows" :key="a.id" class="flex flex-col gap-1 px-4 py-3">
      <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <RouterLink
          v-if="proj"
          :to="{ name: 'admin-project', params: { projectId: proj.id } }"
          class="text-sm font-medium text-primary hover:underline"
          >{{ proj.name }}</RouterLink
        >
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
        <span class="ms-auto flex flex-wrap gap-x-3 text-sm text-muted">
          <time :datetime="a.started" :title="new Date(a.started).toLocaleString('en-GB')">{{
            relative(a.started)
          }}</time>
          <span>{{ duration(a) }}</span>
          <time
            v-if="a.finished"
            data-finished
            :datetime="a.finished"
            :title="new Date(a.finished).toLocaleString('en-GB')"
            >finished {{ relative(a.finished) }}</time
          >
        </span>
      </div>
      <p v-if="a.status === 'error' && a.error" class="text-sm text-error">
        {{ a.error.message }}
      </p>
      <TechnicalDetails
        v-if="details && a.status === 'error' && a.error"
        :action-id="a.id"
        :detail="admin ? inlineDetail(a) : undefined"
      />
    </li>
  </ul>
</template>
