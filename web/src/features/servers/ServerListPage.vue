<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { ApiError, errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import StatusDot from '@/components/StatusDot.vue'
import { useCurrentProject } from '@/lib/project'
import { serverStatus } from '@/lib/status'
import LabelChips from './LabelChips.vue'
import { useServers } from './queries'

const { projectId } = useCurrentProject()
const newRoute = computed(() => ({ name: 'server-new', params: { projectId: projectId.value } }))
const route = useRoute()
const router = useRouter()
const selector = computed(() => (typeof route.query.labels === 'string' ? route.query.labels : ''))
const filter = ref(selector.value)
const { data: servers, isPending, error } = useServers(selector)
const filterError = computed(() =>
  error.value instanceof ApiError && error.value.status === 422 ? error.value.message : null,
)
const listError = computed(() => (filterError.value ? null : error.value))

let timer: ReturnType<typeof setTimeout> | undefined
function commit() {
  clearTimeout(timer)
  const v = filter.value.trim()
  if (v === selector.value) return
  void router.replace({ query: { ...route.query, labels: v || undefined } })
}
function clear() {
  filter.value = ''
  commit()
}
watch(filter, () => {
  clearTimeout(timer)
  timer = setTimeout(commit, 400)
})
watch(selector, (v) => {
  if (v !== filter.value.trim()) filter.value = v
})
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="flex flex-col gap-4">
    <div class="flex items-center justify-between gap-3">
      <h1 class="text-xl font-bold tracking-tight text-highlighted">Servers</h1>
      <UButton v-if="servers?.length" :to="newRoute">Create server</UButton>
    </div>

    <form
      v-if="servers?.length || selector || filter"
      role="search"
      novalidate
      @submit.prevent="commit"
    >
      <UFormField name="labels" label="Filter by label" :error="filterError ?? undefined">
        <UInput
          v-model="filter"
          name="labels"
          placeholder="env=prod,team"
          autocomplete="off"
          class="w-full sm:max-w-sm"
          :ui="{ trailing: 'pe-1' }"
        >
          <template v-if="filter" #trailing>
            <UButton
              color="neutral"
              variant="link"
              size="sm"
              icon="i-lucide-circle-x"
              aria-label="Clear filter"
              @click="clear"
            />
          </template>
        </UInput>
      </UFormField>
    </form>

    <UAlert
      v-if="listError"
      color="error"
      variant="subtle"
      :description="errorMessage(listError)"
    />
    <p v-else-if="isPending" class="text-muted">Loading servers…</p>
    <p v-else-if="filterError" class="text-muted">Fix the filter to see servers.</p>
    <p v-else-if="!servers?.length && selector" class="text-muted">No servers match this filter.</p>
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
          <span class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <span class="truncate font-semibold text-highlighted">{{ s.name }}</span>
            <LabelChips :labels="s.labels" :max="3" />
          </span>
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
