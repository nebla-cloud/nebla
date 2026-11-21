<script setup lang="ts">
import { computed, provide, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import { useToast } from '@nuxt/ui/composables/useToast'
import { api } from '@/api/client'
import { ApiError, errorMessage, unwrap } from '@/api/errors'
import type { Action } from '@/api/types'
import ActionProgress from '@/components/ActionProgress.vue'
import { actionDone } from '@/components/actionLabel'
import PageHeader from '@/components/PageHeader.vue'
import Panel from '@/components/Panel.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { useCrumbs } from '@/lib/crumbs'
import { useCurrentProject, parseProjectId } from '@/lib/project'
import { useActionPoll } from '@/lib/useActionPoll'
import LabelChips from './LabelChips.vue'
import PowerActions from './PowerActions.vue'
import { useServer } from './queries'
import { serverBusyKey } from './shell'

const tabs = [
  { name: 'server', label: 'Overview' },
  { name: 'server-graphs', label: 'Graphs' },
  { name: 'server-console', label: 'Console' },
  { name: 'server-rebuild', label: 'Rebuild' },
  { name: 'server-resize', label: 'Resize' },
  { name: 'server-firewalls', label: 'Firewalls' }, // redesign: remove, serverTab meta covers it
  { name: 'server-volumes', label: 'Volumes' }, // redesign: remove, serverTab meta covers it
  { name: 'server-activity', label: 'Activity' },
]

const route = useRoute()
const router = useRouter()
const toast = useToast()
const qc = useQueryClient()
const { projectId } = useCurrentProject()
const serverId = computed(() => parseProjectId(route.params.serverId) ?? 0)
const actionId = ref<number | null>(null)
const lastName = ref('')
const { action, running, error: actionError } = useActionPoll(actionId, { onDone: finished })
const { data: server, error: loadError } = useServer(serverId, running)
provide(serverBusyKey, running)

useCrumbs(() => [
  { label: 'Servers', to: { name: 'servers', params: { projectId: projectId.value } } },
  ...(server.value ? [{ label: server.value.name }] : []),
])

// The create page hands its create_server action over in history state.
const handoff = router.options.history.state.actionId
if (typeof handoff === 'number') {
  void router.replace({ path: route.fullPath, state: { actionId: null }, force: true })
  void adoptHandoff(handoff, serverId.value)
}

async function adoptHandoff(id: number, sid: number) {
  try {
    const a = await qc.fetchQuery({
      queryKey: ['action', id],
      queryFn: async (): Promise<Action> =>
        unwrap(await api.GET('/actions/{id}', { params: { path: { id } } })),
      staleTime: Infinity,
    })
    const mine = a.resources.some((r) => r.type === 'server' && r.id === sid)
    if (mine && actionId.value === null && serverId.value === sid) actionId.value = id
  } catch {
    // A handoff that can't be read is dropped; the server status still shows.
  }
}

function started(a: Action) {
  lastName.value = server.value?.name ?? ''
  // Seed so the buttons lock now, and mark stale so the first poll still runs at once.
  qc.setQueryData(['action', a.id], a)
  void qc.invalidateQueries({ queryKey: ['action', a.id], refetchType: 'none' })
  actionId.value = a.id
  void qc.invalidateQueries({ queryKey: ['actions', projectId.value] })
  if (
    (route.name === 'server-rebuild' || route.name === 'server-resize') &&
    (a.command === 'rebuild_server' || a.command === 'change_server_type')
  )
    void router.push({
      name: 'server',
      params: { projectId: projectId.value, serverId: serverId.value },
    })
}

async function finished(a: Action) {
  const name = lastName.value || server.value?.name || ''
  if (a.command === 'delete_server') {
    toast.add({ title: `${name} deleted`, color: 'success' })
    await router.push({ name: 'servers', params: { projectId: projectId.value } })
    return
  }
  const verb = actionDone(a.command)
  if (verb) toast.add({ title: `${name} ${verb}`, color: 'success' })
}

watch(
  () => (action.value?.status === 'error' ? action.value.id : null),
  (id) => {
    if (id === null) return
    void qc.invalidateQueries({ queryKey: ['server', projectId.value, serverId.value] })
    void qc.invalidateQueries({ queryKey: ['servers', projectId.value] })
    void qc.invalidateQueries({ queryKey: ['actions', projectId.value] })
  },
)

const notFound = computed(
  () => !server.value && loadError.value instanceof ApiError && loadError.value.status === 404,
)
</script>

<template>
  <div class="flex flex-col gap-5">
    <UAlert
      v-if="notFound"
      color="error"
      variant="subtle"
      description="This server doesn't exist."
    />
    <UAlert
      v-else-if="loadError && !server"
      color="error"
      variant="subtle"
      :description="errorMessage(loadError)"
    />
    <p v-else-if="!server" class="text-muted">Loading server…</p>

    <template v-else>
      <div class="flex flex-col gap-3">
        <PageHeader
          :title="server.name"
          class="max-sm:flex-col max-sm:items-stretch sm:items-center"
        >
          <template #meta>
            <StatusBadge :status="server.status" class="shrink-0" />
          </template>
          <template #actions>
            <UButton
              color="neutral"
              variant="outline"
              icon="i-lucide-terminal"
              :to="{ name: 'server-console', params: { projectId, serverId } }"
            >
              Console
            </UButton>
            <PowerActions :server="server" :busy="running" @started="started" />
          </template>
        </PageHeader>
        <LabelChips v-if="Object.keys(server.labels).length" :labels="server.labels" :max="8" />
      </div>

      <Panel v-if="action" as="div">
        <ActionProgress :action="action" />
      </Panel>
      <UAlert v-if="actionError" color="error" variant="subtle" :description="actionError" />

      <nav aria-label="Server sections" class="overflow-x-auto border-b border-default">
        <ul class="flex min-w-max gap-1">
          <li v-for="t in tabs" :key="t.name">
            <RouterLink
              :to="{ name: t.name, params: { projectId, serverId } }"
              class="-mb-px inline-block border-b-2 border-transparent px-3 py-2 text-sm font-medium text-muted hover:text-highlighted focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-primary aria-[current=page]:border-inverted aria-[current=page]:font-semibold aria-[current=page]:text-highlighted"
            >
              {{ t.label }}
            </RouterLink>
          </li>
        </ul>
      </nav>

      <RouterView :server="server" @started="started" />
    </template>
  </div>
</template>
