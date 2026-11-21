<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import UAlert from '@nuxt/ui/components/Alert.vue'
import UButton from '@nuxt/ui/components/Button.vue'
import UFormField from '@nuxt/ui/components/FormField.vue'
import UInput from '@nuxt/ui/components/Input.vue'
import { ApiError, errorMessage } from '@/api/errors'
import EmptyState from '@/components/EmptyState.vue'
import ListGroup from '@/components/ListGroup.vue'
import ListRow from '@/components/ListRow.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusArch from '@/components/StatusArch.vue'
import { useCurrentProject } from '@/lib/project'
import { serverStatus } from '@/lib/status'
import LabelChips from './LabelChips.vue'
import OsBadge from './OsBadge.vue'
import { useServers } from './queries'

const { projectId, project } = useCurrentProject()
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

const description = computed(() => {
  const list = servers.value
  if (!list?.length || !project.value) return undefined
  const running = list.filter((s) => s.status === 'running').length
  const n = list.length
  return `${n} ${n === 1 ? 'server' : 'servers'} in ${project.value.name}, ${running} running`
})

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
    <PageHeader title="Servers" :description="description">
      <template v-if="servers?.length" #actions>
        <UButton :to="newRoute">Create server</UButton>
      </template>
    </PageHeader>

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
          size="sm"
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
    <ListGroup v-else>
      <ListRow
        v-for="s in servers"
        :key="s.id"
        :to="{ name: 'server', params: { projectId, serverId: s.id } }"
      >
        <template #leading><StatusArch :status="s.status" /></template>
        <span class="flex min-w-0 items-center gap-x-2">
          <span class="truncate">{{ s.name }}</span>
          <LabelChips :labels="s.labels" :max="3" />
        </span>
        <template #meta>
          <span
            class="flex flex-wrap gap-x-4 sm:grid sm:w-[28rem] sm:grid-cols-[1.4fr_1fr_1.1fr_0.8fr] sm:gap-x-3 lg:w-[38rem] [&>*]:min-w-0 [&>*]:truncate"
          >
            <span class="flex items-center gap-2">
              <OsBadge :name="s.image.os_flavor" />
              <span class="truncate">{{ s.image.name }}</span>
            </span>
            <span>{{ s.server_type.name }}</span>
            <span v-if="s.status === 'running'" class="nb-mono">{{
              s.public_net.ipv4?.ip ?? '—'
            }}</span>
            <span v-else>{{ serverStatus(s.status).label }}</span>
            <span>{{ s.location.name }}</span>
          </span>
        </template>
      </ListRow>
    </ListGroup>
  </div>
</template>
